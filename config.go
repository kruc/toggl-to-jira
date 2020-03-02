package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type clientConfig struct {
	jiraUsername   string
	jiraPassword   string
	jiraClientUser string
	jiraHost       string
	stachurskyMode int
}

type globalConfigType struct {
	defaultClient           clientConfig
	period                  int
	logFormat               string
	logOutput               string
	jiraMigrationSuccessTag string
	jiraMigrationFailedTag  string
	jiraMigrationSkipTag    string
}

func checkTogglToken() bool {
	log.Info("Checking configuration...")

	configPath := "toggl_token"
	if viper.IsSet(configPath) && viper.GetString(configPath) != "FILL_IT" {
		return true
	}

	log.Info("Generating config template for %v\n", configPath)
	viper.Set(fmt.Sprintf("%v", configPath), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "default_client.jira_host"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "default_client.jira_password"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "default_client.jira_username"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "default_client.jira_client_user"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "default_client.stachursky_mode"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v", "log_format"), "text")
	viper.Set(fmt.Sprintf("%v", "log_output"), "stdout")
	viper.Set(fmt.Sprintf("%v", "jira_migration_success_tag"), "logged")
	viper.Set(fmt.Sprintf("%v", "jira_migration_failed_tag"), "jira-migration-failed")
	viper.Set(fmt.Sprintf("%v", "jira_migration_skip_tag"), "jira-migration-skip")
	viper.Set(fmt.Sprintf("%v", "period"), 1)

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

func generateClientConfigTemplate(configPath string) {
	fmt.Printf("Generating config template for %v...\n", configPath)
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_username"), "FILL_IT OR REMOVE TO USE DEFAULT_CLIENT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_password"), "FILL_IT OR REMOVE TO USE DEFAULT_CLIENT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_client_user"), "FILL_IT OR REMOVE TO USE DEFAULT_CLIENT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_host"), "FILL_IT OR REMOVE TO USE DEFAULT_CLIENT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "stachursky_mode"), "FILL_IT OR REMOVE TO USE DEFAULT_CLIENT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "enabled"), false)
	err := viper.WriteConfig()

	if err != nil {
		log.WithFields(log.Fields{
			"configPath": configPath,
		}).Error(err)
		return
	}
	log.WithFields(log.Fields{
		"configPath": configPath,
	}).Info("Client config template created!\n")
}

func parseGlobalConfig() globalConfigType {
	clientDefaultConfigPath := "default_client"

	globalConfig := globalConfigType{
		defaultClient:           parseClientConfig(clientDefaultConfigPath, globalConfig),
		period:                  viper.GetInt("period"),
		logFormat:               viper.GetString("log_format"),
		logOutput:               viper.GetString("log_output"),
		jiraMigrationSuccessTag: viper.GetString("jira_migration_success_tag"),
		jiraMigrationFailedTag:  viper.GetString("jira_migration_failed_tag"),
		jiraMigrationSkipTag:    viper.GetString("jira_migration_skip_tag"),
	}

	return globalConfig
}

func parseClientConfig(clientConfigPath string, globalConfig globalConfigType) clientConfig {

	clientConfig := clientConfig{
		jiraUsername:   getString("jira_username", clientConfigPath, globalConfig.defaultClient.jiraUsername),
		jiraPassword:   getString("jira_password", clientConfigPath, globalConfig.defaultClient.jiraPassword),
		jiraClientUser: getString("jira_client_user", clientConfigPath, globalConfig.defaultClient.jiraClientUser),
		jiraHost:       getString("jira_host", clientConfigPath, globalConfig.defaultClient.jiraHost),
		stachurskyMode: getInt("stachursky_mode", clientConfigPath, globalConfig.defaultClient.stachurskyMode),
	}

	if flag.CommandLine.Changed("tryb-niepokorny") {
		clientConfig.stachurskyMode = stachurskyMode
	}

	return clientConfig
}

func getString(key, clientConfigPath, defaultValue string) string {
	if viper.IsSet(fmt.Sprintf("%v.%v", clientConfigPath, key)) {
		return viper.GetString(fmt.Sprintf("%v.%v", clientConfigPath, key))
	}

	return defaultValue
}

func getInt(key, clientConfigPath string, defaultValue int) int {
	if viper.IsSet(fmt.Sprintf("%v.%v", clientConfigPath, key)) {
		return viper.GetInt(fmt.Sprintf("%v.%v", clientConfigPath, key))
	}

	return defaultValue
}
