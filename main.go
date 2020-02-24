package main

import (
	"fmt"
	"os"
	s "strings"
	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/kruc/gtoggl"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type togglData struct {
	client           string
	project          string
	issueID          string
	issueComment     string
	started          time.Time
	timeSpentSeconds int
}

type clientConfig struct {
	jiraUsername   string
	jiraPassword   string
	jiraClientUser string
	jiraHost       string
}

var (
	period               int
	config               = "config"
	configPath           string
	logFormat            string
	logOutput            string
	jiraMigrationSuccess string
	logFile              *os.File
	jiraMigrationFail    = "jira-migration-failed"
	stachurskyMode       int
	debugMode            bool
	version              bool

	BuildVersion string
	BuildDate    string
	GitCommit    string
	GitAuthor    string
)

func init() {
	flag.IntVarP(&period, "period", "p", 1, "Migrate time entries from last given days")
	flag.StringVarP(&configPath, "config-path", "c", fmt.Sprintf("%v/.toggl-to-jira", os.Getenv("HOME")), "Config file path")
	flag.StringVarP(&logFormat, "format", "f", "text", "Log format (text|json)")
	flag.StringVarP(&logOutput, "output", "o", "stdout", "Log filename ")
	flag.StringVarP(&jiraMigrationSuccess, "logged-tag", "l", "logged", "Toggl logged tag")
	flag.IntVarP(&stachurskyMode, "tryb-niepokorny", "t", 1, "Rounding up the value of logged time up (minutes)")
	flag.BoolVar(&debugMode, "debug", false, "Debug mode - display workload but not update jiras")
	flag.BoolVarP(&version, "version", "v", false, "Display version")
	flag.Parse()
	// Prepare config
	os.MkdirAll(configPath, 0755)
	os.OpenFile(fmt.Sprintf("%v/%v.yaml", configPath, config), os.O_CREATE|os.O_RDWR, 0666)

	viper.SetConfigName(config)
	viper.AddConfigPath(configPath)
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s", err))
	}

	// Prepare logger
	log.SetFormatter(&log.TextFormatter{})
	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}

	log.SetOutput(os.Stdout)
	if logOutput != "stdout" {
		logFile, _ := os.OpenFile(logOutput, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		log.SetOutput(logFile)
	}
	log.SetLevel(log.InfoLevel)
}
func main() {
	defer logFile.Close()
	if version {
		displayVersion()
		return
	}
	if !checkTogglToken() {
		log.Error("Please provide valid toggl_token visit => https://www.toggl.com/app/profile")
		return
	}

	tc, err := gtoggl.NewClient(viper.GetString("toggl_token"))
	tec := tc.TimeentryClient
	cc := tc.TClient
	pc := tc.ProjectClient

	current := time.Now()
	start := current.Add(time.Hour * 24 * time.Duration(period) * -1)

	timeEntries, err := tec.GetRange(start, current)
	if err != nil {
		log.Error(err)
		return
	}

	for _, timeEntry := range timeEntries {
		if find(timeEntry.Tags, jiraMigrationSuccess) && !debugMode {
			continue
		}

		log.Info(fmt.Sprintf("Start processing %v: %v", timeEntry.Id, timeEntry.Description))

		project, err := pc.Get(timeEntry.Pid)
		if err != nil {
			log.WithFields(log.Fields{
				"timeEntry": timeEntry,
				"reason":    "Probably time entry is not assign to project in Toggl",
				"solution":  "Edit time entry in toggl and assign it to project",
			}).Error(err)
			continue
		}

		client, err := cc.Get(project.CId)

		if err != nil {
			log.WithFields(log.Fields{
				"timeEntry": timeEntry,
			}).Error(err)
			continue
		}

		timeSpentSeconds := dosko(getTimeDiff(timeEntry.Start, timeEntry.Stop))

		togglData := togglData{
			client:           s.ToLower(client.Name),
			project:          s.ToLower(project.Name),
			issueID:          parseIssueID(timeEntry.Description),
			issueComment:     parseIssueComment(timeEntry.Description),
			started:          adjustTogglDate(timeEntry.Start),
			timeSpentSeconds: timeSpentSeconds,
		}

		clientConfigPath := fmt.Sprintf("client.%v", togglData.client)

		if !viper.IsSet(clientConfigPath) {
			generateClientConfigTemplate(clientConfigPath)
			continue
		}

		if viper.IsSet(fmt.Sprintf("%v.%v", clientConfigPath, "config_check")) {
			log.Warnf("Don't forget to remove config_check field from %v configuration file", clientConfigPath)
			continue
		}

		clientConfig := clientConfig{
			jiraUsername:   viper.GetString(fmt.Sprintf("%v.%v", clientConfigPath, "jira_username")),
			jiraPassword:   viper.GetString(fmt.Sprintf("%v.%v", clientConfigPath, "jira_password")),
			jiraClientUser: viper.GetString(fmt.Sprintf("%v.%v", clientConfigPath, "jira_client_user")),
			jiraHost:       viper.GetString(fmt.Sprintf("%v.%v", clientConfigPath, "jira_host")),
		}

		// JIRA PART
		tp := jira.BasicAuthTransport{
			Username: clientConfig.jiraUsername,
			Password: clientConfig.jiraPassword,
		}

		jiraClient, _ := jira.NewClient(tp.Client(), clientConfig.jiraHost)

		tt := jira.Time(togglData.started)
		worklogRecord := jira.WorklogRecord{
			Comment:          togglData.issueComment,
			TimeSpentSeconds: togglData.timeSpentSeconds,
			Started:          &tt,
		}
		if debugMode {
			fmt.Printf("%+v\n", worklogRecord)
		}
		if debugMode == false {

			jwr, jr, err := jiraClient.Issue.AddWorklogRecord(togglData.issueID, &worklogRecord)

			if err != nil {
				log.WithFields(log.Fields{
					"worklogRecord": jwr,
					"response":      jr,
				}).Error(err)
				timeEntry.Tags = append(timeEntry.Tags, jiraMigrationFail)
				log.Info(fmt.Sprintf("Add %v tag", jiraMigrationFail))
			} else {
				log.Info(fmt.Sprintf("Jira workload added"))
				timeEntry.Tags = removeTag(timeEntry.Tags, jiraMigrationFail)
				timeEntry.Tags = append(timeEntry.Tags, jiraMigrationSuccess)
				log.Info(fmt.Sprintf("Add %v tag", jiraMigrationSuccess))
			}
			te, err := tec.Update(&timeEntry)

			if err != nil {
				log.WithFields(log.Fields{
					"timeEntry": te,
				}).Error(err)
			}
			log.Info(fmt.Sprintf("Finish processing %v: %v", timeEntry.Id, timeEntry.Description))
		}
	}
}

func dosko(timeSpentSeconds int) int {

	d, err := time.ParseDuration(fmt.Sprintf("%vs", timeSpentSeconds))
	if err != nil {
		panic(err)
	}

	stachurskyFactor := time.Duration(stachurskyMode) * time.Minute
	roundedValue := d.Round(stachurskyFactor)

	if int(roundedValue.Seconds()) == 0 {
		roundedValue = stachurskyFactor
	}

	if debugMode {
		fmt.Printf("%s - toggl value\n", d.String())
		fmt.Printf("%s - stachursky mode (%vm) \n", roundedValue.String(), stachurskyMode)
	}

	return int(roundedValue.Seconds())
}

func removeTag(tagsList []string, tagToRemove string) []string {
	for i := 0; i < len(tagsList); i++ {
		if tagsList[i] == tagToRemove {
			tagsList = append(tagsList[:i], tagsList[i+1:]...)
			i-- // form the remove item index to start iterate next item
		}
	}
	return tagsList
}
func adjustTogglDate(togglDate time.Time) time.Time {

	togglDate = togglDate.Add(time.Millisecond * 1)
	return togglDate
}

func parseIssueID(value string) string {
	fields := s.Fields(value)

	return fields[0]
}
func parseIssueComment(value string) string {
	fields := s.Fields(value)

	return s.Join(fields[1:], " ")
}

func getTimeDiff(start, stop time.Time) int {
	return int(stop.Sub(start).Seconds())
}

func find(a []string, x string) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}

	return false
}

func checkTogglToken() bool {
	log.Info("Checking configuration...")

	configPath := "toggl_token"
	if viper.IsSet(configPath) && viper.GetString(configPath) != "FILL_IT" {
		return true
	}
	log.Info("Generating config template for %v\n", configPath)
	viper.Set(fmt.Sprintf("%v", configPath), "FILL_IT")

	err := viper.WriteConfig()
	if err != nil {
		log.WithFields(log.Fields{
			"configPath": configPath,
		}).Error(err)
		return false
	}

	log.WithFields(log.Fields{
		"configPath": configPath,
	}).Info("Client config template created!\n")

	return false
}

func displayVersion() {
	fmt.Printf("BuildVersion: %s\tBuildDate: %s\tGitCommit: %s\tGitAuthor: %s\n", BuildVersion, BuildDate, GitCommit, GitAuthor)
}
