// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/cheggaaa/pb/v3"
	"github.com/spf13/cobra"
)

func getToitSDKURL(version string) string {
	currOS := runtime.GOOS
	if currOS == "darwin" {
		currOS = "macos"
	}
	return fmt.Sprintf("https://github.com/toitlang/toit/releases/download/%s/toit-%s.tar.gz", version, currOS)
}

func SetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "setup",
		Short:        "Setup the toit SDK",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := cmd.Flags().GetString("version")
			if err != nil {
				return err
			}

			sdkPath, err := GetSDKCachePath()
			if err != nil {
				return err
			}

			sdkURL := getToitSDKURL(version)
			fmt.Printf("Downloading toit SDK from %s...\n", sdkURL)
			ctx := cmd.Context()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, getToitSDKURL(version), nil)
			if err != nil {
				return err
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return fmt.Errorf("Failed downloading the toit SDK artifact: %v", resp.Status)
			}

			progress := pb.New64(resp.ContentLength)
			r := progress.Start().NewProxyReader(resp.Body)

			gzipReader, err := newGZipReader(r)
			if err != nil {
				r.Close()
				return fmt.Errorf("failed to read the toit SDK artifact as gzip file: %w", err)
			}
			defer gzipReader.Close()

			if err := os.RemoveAll(sdkPath); err != nil {
				return err
			}

			if err := os.MkdirAll(sdkPath, 0755); err != nil {
				return err
			}

			if err := extractTarFile(gzipReader, sdkPath, "toit/"); err != nil {
				return fmt.Errorf("failed to extract the toit SDK, reason: %w", err)
			}
			gzipReader.Close()
			fmt.Println("Successfully installed toit SDK into", sdkPath)
			return nil
		},
	}

	cmd.Flags().StringP("version", "v", "v0.0.1", "the version of the toit SDK to download")
	return cmd
}
