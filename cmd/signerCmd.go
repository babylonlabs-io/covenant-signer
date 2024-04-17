package cmd

import (
	"context"
	"fmt"

	"github.com/babylonchain/covenant-signer/internal/config"
	"github.com/babylonchain/covenant-signer/internal/logger"
	"github.com/babylonchain/covenant-signer/internal/services/signer"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(runSignerCmd)
}

var runSignerCmd = &cobra.Command{
	Use:   "start",
	Short: "starts the signer service",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := cmd.Flags().GetString(configPathKey)
		if err != nil {
			return err
		}
		fmt.Println(path)
		cfg, err := config.GetConfig(path)
		fmt.Println(cfg)
		if err != nil {
			return err
		}
		ctx := context.Background()
		log := logger.DefaultLogger()
		srv, err := signer.New(
			ctx,
			log,
			nil,
		)

		if err != nil {
			return err
		}

		log.Info("Starting signer service")
		// TODO: Add signal handling and gracefull shutdown
		return srv.Start()
	},
}
