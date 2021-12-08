package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func RunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run <entrypoint>",
		Short:        "run toit code",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := GetConfig()
			if err != nil {
				return err
			}

			ctx := context.Background()
			device, err := GetDevice(ctx, cfg, true)
			if err != nil {
				return err
			}

			toitc, ok := os.LookupEnv(ToitcPathEnv)
			if !ok {
				return fmt.Errorf("You must set the env variable '%s'", ToitcPathEnv)
			}

			toitvm, ok := os.LookupEnv(ToitvmPathEnv)
			if !ok {
				return fmt.Errorf("You must set the env variable '%s'", ToitvmPathEnv)
			}

			toits2i, ok := os.LookupEnv(ToitSnap2ImagePathEnv)
			if !ok {
				return fmt.Errorf("You must set the env variable '%s'", ToitSnap2ImagePathEnv)
			}

			snapshot, err := os.CreateTemp("", "*.snap")
			if err != nil {
				return err
			}
			snapshot.Close()
			defer os.Remove(snapshot.Name())

			entrypoint := args[0]
			buildSnap := exec.CommandContext(ctx, toitc, "-w", snapshot.Name(), entrypoint)
			buildSnap.Stderr = os.Stderr
			buildSnap.Stdout = os.Stdout
			if err := buildSnap.Run(); err != nil {
				return err
			}

			image, err := os.CreateTemp("", "*.image")
			if err != nil {
				return err
			}
			image.Close()
			defer os.Remove(image.Name())

			buildImage := exec.CommandContext(ctx, toitvm, toits2i, "--binary", snapshot.Name(), image.Name())
			buildImage.Stderr = os.Stderr
			buildImage.Stdout = os.Stdout

			image, err = os.Open(image.Name())
			if err != nil {
				return err
			}
			defer image.Close()
			return device.Run(image)
		},
	}

	cmd.Flags().UintP("port", "p", scanPort, "UDP Port to scan for devices on")
	cmd.Flags().DurationP("timeout", "t", scanTimeout, "How long to scan")
	return cmd
}
