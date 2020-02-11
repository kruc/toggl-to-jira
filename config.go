package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func generateClientConfigTemplate(configPath string) {
	fmt.Printf("Generating config template for %v...\n", configPath)
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_username"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_password"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_client_user"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "jira_host"), "FILL_IT")
	viper.Set(fmt.Sprintf("%v.%v", configPath, "config_check"), "(Remove it after fill client config)")
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
