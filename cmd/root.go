package cmd

import (
	"context"
	"github.com/MetaSyntactical/voyager-traefik-manager/worker"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
)

var (
	containerId       string
	traefikConfigFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "voyager-traefik-manager",
	Version: "unknown",
	Short:   "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())

		w, err := worker.New(containerId, traefikConfigFile, ctx, cancel)
		if err != nil {
			panic(err)
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			select {
			case <-c:
				err := w.Stop()
				if err != nil {
					panic(err)
				}
			}
		}()

		w.Start()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(version string) {
	rootCmd.Version = version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.voyager-traefik-manager.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.Flags().StringVar(&containerId, "containerId", "", "id of the current container")
	rootCmd.Flags().StringVar(&traefikConfigFile, "traefikConfig", "", "path to traefik config file, if empty - stdout")
}
