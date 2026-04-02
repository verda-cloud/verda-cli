package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github/verda-cloud/verda-cli/internal/verda-cli/options"
)

const defaultConfigName = "config"

// initConfig sets up viper to read configuration from file, environment
// variables, and flags (in ascending priority order).
func initConfig(cfgFile string) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		dir, err := options.VerdaDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(dir)
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
