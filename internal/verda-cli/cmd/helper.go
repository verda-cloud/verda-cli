package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultConfigName = "config"

// initConfig sets up viper to read configuration from file, environment
// variables, and flags (in ascending priority order).
func initConfig(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(filepath.Join(home, ".verda"))
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(defaultConfigName)
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("VERDA")

	replacer := strings.NewReplacer(".", "_", "-", "_")
	viper.SetEnvKeyReplacer(replacer)

	_ = viper.ReadInConfig()
}
