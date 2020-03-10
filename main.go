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

var (
	globalConfig   globalConfigType
	period         int
	config         = "config"
	configPath     string
	logFormat      string
	logOutput      string
	logFile        *os.File
	stachurskyMode int
	applyMode      bool
	version        bool
	// For version info
	BuildVersion string
	BuildDate    string
	GitCommit    string
)

func init() {
	flag.BoolVar(&applyMode, "apply", false, "Update jira tasks workloads")
	flag.StringVarP(&configPath, "config-path", "c", fmt.Sprintf("%v/.toggl-to-jira", os.Getenv("HOME")), "Config file path")

	// Prepare config
	os.MkdirAll(configPath, 0755)
	os.OpenFile(fmt.Sprintf("%v/%v.yaml", configPath, config), os.O_CREATE|os.O_RDWR, 0666)

	viper.SetConfigName(config)
	viper.AddConfigPath(configPath)
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s", err))
	}

	globalConfig = parseGlobalConfig()

	flag.IntVarP(&period, "period", "p", globalConfig.period, "Migrate time entries from last given days")
	flag.StringVarP(&logFormat, "format", "f", globalConfig.logFormat, "Log format (text|json)")
	flag.StringVarP(&logOutput, "output", "o", globalConfig.logOutput, "Log output (stdout|filename)")
	flag.IntVarP(&stachurskyMode, "tryb-niepokorny", "t", globalConfig.defaultClient.stachurskyMode, "Rounding up the value of logged time up (minutes)")
	flag.BoolVarP(&version, "version", "v", false, "Display version")
	flag.Parse()

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
		if find(timeEntry.Tags, globalConfig.jiraMigrationSuccessTag) || find(timeEntry.Tags, globalConfig.jiraMigrationSkipTag) {
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

		clientConfigPath := fmt.Sprintf("client.%v", s.ToLower(client.Name))

		if !viper.IsSet(clientConfigPath) {
			generateClientConfigTemplate(clientConfigPath)
			continue
		}

		if !viper.GetBool(fmt.Sprintf("%v.%v", clientConfigPath, "enabled")) {
			log.Warnf("Don't forget to enable client (set %v.enabled = true)", clientConfigPath)
			continue
		}

		clientConfig := parseClientConfig(clientConfigPath, globalConfig)

		timeSpentSeconds := dosko(getTimeDiff(timeEntry.Start, timeEntry.Stop), clientConfig.stachurskyMode)

		togglData := togglData{
			client:           s.ToLower(client.Name),
			project:          s.ToLower(project.Name),
			issueID:          parseIssueID(timeEntry.Description),
			issueComment:     parseIssueComment(timeEntry.Description),
			started:          adjustTogglDate(timeEntry.Start),
			timeSpentSeconds: timeSpentSeconds,
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
		issueURL := fmt.Sprintf("%v/browse/%v", clientConfig.jiraHost, togglData.issueID)
		if !applyMode {
			fmt.Println("\nWorkload details:")
			fmt.Printf("Time spent: %+v\n", time.Duration(worklogRecord.TimeSpentSeconds)*time.Second)
			fmt.Printf("Comment: %+v\n", worklogRecord.Comment)
			fmt.Printf("Issue url: %v\n", issueURL)
			fmt.Println("------------------------")
		}
		if applyMode == true {

			jwr, jr, err := jiraClient.Issue.AddWorklogRecord(togglData.issueID, &worklogRecord)

			if err != nil {
				log.WithFields(log.Fields{
					"worklogRecord": jwr,
					"response":      jr,
				}).Error(err)

				timeEntry.Tags = append(timeEntry.Tags, globalConfig.jiraMigrationFailedTag)
				log.Info(fmt.Sprintf("Add %v tag", globalConfig.jiraMigrationFailedTag))
			} else {
				log.Info(fmt.Sprintf("Jira workload added"))
				timeEntry.Tags = removeTag(timeEntry.Tags, globalConfig.jiraMigrationFailedTag)
				timeEntry.Tags = append(timeEntry.Tags, globalConfig.jiraMigrationSuccessTag)
				log.Info(fmt.Sprintf("Add %v tag", globalConfig.jiraMigrationSuccessTag))
			}
			log.Info(fmt.Sprintf("Issue url: %v", issueURL))
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

func dosko(timeSpentSeconds, stachurskyMode int) int {

	d, err := time.ParseDuration(fmt.Sprintf("%vs", timeSpentSeconds))
	if err != nil {
		panic(err)
	}

	stachurskyFactor := time.Duration(stachurskyMode) * time.Minute
	roundedValue := d.Round(stachurskyFactor)

	if int(roundedValue.Seconds()) == 0 {
		roundedValue = stachurskyFactor
	}

	if !applyMode {
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

func displayVersion() {
	fmt.Printf("BuildVersion: %s\tBuildDate: %s\tGitCommit: %s\n", BuildVersion, BuildDate, GitCommit)
}
