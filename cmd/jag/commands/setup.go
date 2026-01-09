// Copyright (C) 2021 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/toitlang/jaguar/cmd/jag/directory"
)

// New helper functions

func isSDKCurrent(sdkPath string, expectedVersion string) (bool, error) {
	versionFile := filepath.Join(sdkPath, "SDK_VERSION")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return false, nil // missing or error → not current
	}
	current := string(bytes.TrimSpace(data))
	return current == expectedVersion, nil
}

func markSDKCurrent(sdkPath string, version string) error {
	versionFile := filepath.Join(sdkPath, "SDK_VERSION")
	return os.WriteFile(versionFile, []byte(version+"\n"), 0644)
}

func isAssetsCurrent(assetsPath string, expectedVersion string) (bool, error) {
	versionFile := filepath.Join(assetsPath, "ASSETS_VERSION")
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return false, nil
	}
	current := string(bytes.TrimSpace(data))
	return current == expectedVersion, nil
}

func markAssetsCurrent(assetsPath string, version string) error {
	versionFile := filepath.Join(assetsPath, "ASSETS_VERSION")
	return os.WriteFile(versionFile, []byte(version+"\n"), 0644)
}

func getToitSDKURL(version string) (string, error) {
	currOS := runtime.GOOS
	currARCH := runtime.GOARCH
	selector := ""
	if currOS == "darwin" {
		if currARCH == "amd64" {
			selector = "macos-x64"
		} else if currARCH == "arm64" {
			selector = "macos-aarch64"
		} else {
			return "", fmt.Errorf("unsupported architecture %s for macOS", currARCH)
		}
	} else if currOS == "linux" {
		if currARCH == "amd64" {
			selector = "linux-x64"
		} else if currARCH == "arm" {
			selector = "linux-armv7"
		} else if currARCH == "arm64" {
			selector = "linux-aarch64"
		} else {
			return "", fmt.Errorf("unsupported architecture %s for Linux", currARCH)
		}
	} else if currOS == "windows" {
		if currARCH == "amd64" {
			selector = "windows-x64"
		} else {
			return "", fmt.Errorf("unsupported architecture %s for Windows", currARCH)
		}
	} else {
		return "", fmt.Errorf("unsupported OS %s", currOS)
	}
	return fmt.Sprintf("https://github.com/toitlang/toit/releases/download/%s/toit-%s.tar.gz", version, selector), nil
}

func getAssetsURL(version string) string {
	return fmt.Sprintf("https://github.com/toitlang/jaguar/releases/download/%s/assets.tar.gz", version)
}

func SetupCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup the Toit SDK and Jaguar assets",
		Long: `Setup the Toit SDK and Jaguar assets by downloading them from GitHub if needed.
The SDK and assets are stored in local cache directories under your home folder.
Use --check to verify the current installation status.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Early flag: print-path
			printPath, err := cmd.Flags().GetString("print-path")
			if err != nil {
				return err
			}
			switch printPath {
			case "assets":
				path, err := directory.GetAssetsCachePath()
				if err != nil {
					return err
				}
				fmt.Println(path)
				return nil
			case "sdk":
				path, err := directory.GetSDKCachePath()
				if err != nil {
					return err
				}
				fmt.Println(path)
				return nil
			case "":
				// proceed normally
			default:
				return fmt.Errorf("invalid value for --print-path: %s (expected 'assets' or 'sdk')", printPath)
			}

			// Get paths upfront
			sdkPath, err := directory.GetSDKCachePath()
			if err != nil {
				return err
			}
			assetsPath, err := directory.GetAssetsCachePath()
			if err != nil {
				return err
			}

			// --check mode: detailed status report
			check, err := cmd.Flags().GetBool("check")
			if err != nil {
				return err
			}
			if check {
				fmt.Println("=== Jaguar Setup Check ===")
				fmt.Printf("Expected Jaguar version: %s\n", info.Version)
				fmt.Printf("Expected Toit SDK version: %s\n\n", info.SDKVersion)

				// SDK status
				if fi, err := os.Stat(sdkPath); os.IsNotExist(err) {
					fmt.Printf("SDK directory: MISSING (%s)\n", sdkPath)
				} else if err != nil {
					fmt.Printf("SDK directory: ERROR checking (%s): %v\n", sdkPath, err)
				} else if !fi.IsDir() {
					fmt.Printf("SDK path exists but is not a directory: %s\n", sdkPath)
				} else {
					if current, _ := isSDKCurrent(sdkPath, info.SDKVersion); current {
						fmt.Printf("SDK: OK (%s at %s)\n", info.SDKVersion, sdkPath)
					} else {
						fmt.Printf("SDK: OUTDATED or corrupted (found in %s)\n", sdkPath)
					}
				}

				// Assets status
				skipAssets, _ := cmd.Flags().GetBool("skip-assets") // ignore error, not critical here
				if skipAssets {
					fmt.Println("Assets: SKIPPED (via --skip-assets)")
				} else if fi, err := os.Stat(assetsPath); os.IsNotExist(err) {
					fmt.Printf("Assets directory: MISSING (%s)\n", assetsPath)
				} else if err != nil {
					fmt.Printf("Assets directory: ERROR checking (%s): %v\n", assetsPath, err)
				} else if !fi.IsDir() {
					fmt.Printf("Assets path exists but is not a directory: %s\n", assetsPath)
				} else {
					if current, _ := isAssetsCurrent(assetsPath, info.Version); current {
						fmt.Printf("Assets: OK (%s at %s)\n", info.Version, assetsPath)
					} else {
						fmt.Printf("Assets: OUTDATED or corrupted (found in %s)\n", assetsPath)
					}
				}

				// Jaguar metadata
				downloaderPath := filepath.Join(sdkPath, "JAGUAR")
				if _, err := os.Stat(downloaderPath); os.IsNotExist(err) {
					fmt.Printf("Jaguar metadata file: MISSING (%s)\n", downloaderPath)
				} else if err != nil {
					fmt.Printf("Jaguar metadata file: ERROR (%v)\n", err)
				} else {
					fmt.Printf("Jaguar metadata file: PRESENT (%s)\n", downloaderPath)
				}

				// Snapshot path
				/* if snapPath, err := directory.GetJaguarSnapshotPath(); err != nil {
					fmt.Printf("Jaguar snapshot: INVALID (%v)\n", err)
				} else {
					fmt.Printf("Jaguar snapshot: OK (%s)\n", snapPath)
				}*/

				// Snapshot path and existence check
				snapPath, err := directory.GetJaguarSnapshotPath()
				if err != nil {
					fmt.Printf("Jaguar snapshot: INVALID (cannot determine path: %v)\n", err)
				} else if _, err := os.Stat(snapPath); os.IsNotExist(err) {
					fmt.Printf("Jaguar snapshot: MISSING (expected at %s)\n", snapPath)
					fmt.Println("   Hint: Run 'jag setup' to install assets, or check your installation.")
				} else if err != nil {
					fmt.Printf("Jaguar snapshot: ERROR checking file (%s): %v\n", snapPath, err)
				} else {
					fmt.Printf("Jaguar snapshot: OK (%s)\n", snapPath)
				}

				// Try to access SDK (calls GetSDK which may validate further)
				if _, err := GetSDK(ctx); err != nil {
					fmt.Printf("SDK accessibility: FAILED (%v)\n", err)
				} else {
					fmt.Println("SDK accessibility: OK")
				}

				fmt.Println("\nCheck complete.")
				return nil
			}

			// Normal setup mode

			// SDK: check and download if needed
			needSDK := true
			if fi, err := os.Stat(sdkPath); err == nil && fi.IsDir() {
				if current, err := isSDKCurrent(sdkPath, info.SDKVersion); err == nil && current {
					fmt.Printf("Toit SDK %s is already installed and up-to-date.\n", info.SDKVersion)
					needSDK = false
				} else {
					fmt.Println("Existing Toit SDK is outdated or corrupted. Re-downloading...")
					os.RemoveAll(sdkPath)
				}
			} else {
				fmt.Printf("Toit SDK not found. Downloading version %s...\n", info.SDKVersion)
			}

			if needSDK {
				if err := downloadSdk(ctx, info.SDKVersion); err != nil {
					return err
				}
				if err := markSDKCurrent(sdkPath, info.SDKVersion); err != nil {
					return err
				}
			}

			// Assets: check and download unless skipped
			skipAssets, err := cmd.Flags().GetBool("skip-assets")
			if err != nil {
				return err
			}

			if !skipAssets {
				needAssets := true
				if fi, err := os.Stat(assetsPath); err == nil && fi.IsDir() {
					if current, err := isAssetsCurrent(assetsPath, info.Version); err == nil && current {
						fmt.Printf("Jaguar assets %s are already installed and up-to-date.\n", info.Version)
						needAssets = false
					} else {
						fmt.Println("Existing Jaguar assets are outdated or corrupted. Re-downloading...")
						os.RemoveAll(assetsPath)
					}
				} else {
					fmt.Printf("Jaguar assets not found. Downloading version %s...\n", info.Version)
				}

				if needAssets {
					if err := downloadAssets(ctx, info.Version); err != nil {
						return err
					}
					if err := markAssetsCurrent(assetsPath, info.Version); err != nil {
						return err
					}
				}
			} else {
				fmt.Println("Skipping assets download (as requested via --skip-assets).")
			}

			// Always update the JAGUAR metadata file
			downloaderPath := filepath.Join(sdkPath, "JAGUAR")
			downloaderBytes, err := json.MarshalIndent(&info, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(downloaderPath, downloaderBytes, 0666); err != nil {
				return err
			}

			fmt.Printf("\nSuccessfully set up Jaguar %s with Toit SDK %s.\n", info.Version, info.SDKVersion)
			fmt.Printf("   SDK location:     %s\n", sdkPath)
			fmt.Printf("   Assets location:  %s\n", assetsPath)
			return nil
		},
	}

	cmd.AddCommand(SetupSdkCmd(info))

	cmd.Flags().BoolP("check", "c", false, "Check the current Jaguar setup status in detail")
	cmd.Flags().BoolP("skip-assets", "s", false, "Skip downloading Jaguar assets")
	cmd.Flags().MarkHidden("skip-assets")
	cmd.Flags().String("print-path", "", "Print the cache path for 'assets' or 'sdk' and exit")
	cmd.Flags().MarkHidden("print-path")

	return cmd
}

/* func SetupSdkCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sdk",
		Short:        "Setup just the SDK",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if len(args) != 1 {
				return fmt.Errorf("takes exactly one argument")
			}
			if err := downloadSdkTo(ctx, info.SDKVersion, args[0]); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}*/

func SetupSdkCmd(info Info) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sdk [path]",
		Short: "Setup just the SDK (optionally to a custom path)",
		Long: `Downloads and installs the Toit SDK version ` + info.SDKVersion + `.
If no path is provided, uses the default Jaguar cache location and skips download if already current.
If a path is provided, installs to that directory (overwriting if necessary).`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			var targetPath string

			if len(args) > 1 {
				return fmt.Errorf("takes at most one argument (the target directory)")
			}

			if len(args) == 1 {
				targetPath = args[0]
			} else {
				// Default cache path
				var err error
				targetPath, err = directory.GetSDKCachePath()
				if err != nil {
					return err
				}
				fmt.Printf("No path provided; using default SDK location: %s\n", targetPath)
			}

			// Check if already current
			needDownload := true
			if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
				if current, err := isSDKCurrent(targetPath, info.SDKVersion); err == nil && current {
					fmt.Printf("Toit SDK %s is already installed and current at %s.\n", info.SDKVersion, targetPath)
					needDownload = false
				} else {
					fmt.Printf("Existing SDK at %s is outdated or corrupted. Re-downloading...\n", targetPath)
					if err := os.RemoveAll(targetPath); err != nil {
						return err
					}
				}
			}

			if needDownload {
				if err := downloadSdkTo(ctx, info.SDKVersion, targetPath); err != nil {
					return err
				}

				// Always mark as current after successful download
				if err := markSDKCurrent(targetPath, info.SDKVersion); err != nil {
					return err
				}
			}

			fmt.Printf("Toit SDK %s is ready at %s\n", info.SDKVersion, targetPath)
			return nil
		},
	}
	return cmd
}

//////

func downloadAssets(ctx context.Context, version string) error {
	assetsPath, err := directory.GetAssetsCachePath()
	if err != nil {
		return err
	}

	assetsURL := getAssetsURL(version)
	fmt.Printf("Downloading Jaguar assets from %s ...\n", assetsURL)
	bundle, err := download(ctx, assetsURL)
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(bundle)
	if err != nil {
		bundle.Close()
		return fmt.Errorf("failed to read Jaguar assets as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(assetsPath); err != nil {
		return err
	}

	if err := os.MkdirAll(assetsPath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, assetsPath, "assets/"); err != nil {
		return fmt.Errorf("failed to extract Jaguar assets, reason: %w", err)
	}

	fmt.Println("Successfully installed Jaguar assets into", assetsPath)
	return nil
}

func downloadSdk(ctx context.Context, version string) error {
	sdkPath, err := directory.GetSDKCachePath()
	if err != nil {
		return err
	}
	return downloadSdkTo(ctx, version, sdkPath)
}

func downloadSdkTo(ctx context.Context, version string, sdkPath string) error {
	sdkURL, err := getToitSDKURL(version)
	if err != nil {
		return err
	}
	fmt.Printf("Downloading Toit SDK from %s ...\n", sdkURL)
	sdk, err := download(ctx, sdkURL)
	if err != nil {
		return err
	}

	gzipReader, err := newGZipReader(sdk)
	if err != nil {
		sdk.Close()
		return fmt.Errorf("failed to read the Toit SDK artifact as gzip file: %w", err)
	}
	defer gzipReader.Close()

	if err := os.RemoveAll(sdkPath); err != nil {
		return err
	}

	if err := os.MkdirAll(sdkPath, 0755); err != nil {
		return err
	}

	if err := extractTarFile(gzipReader, sdkPath, "toit/"); err != nil {
		return fmt.Errorf("failed to extract the Toit SDK, reason: %w", err)
	}
	gzipReader.Close()
	fmt.Println("Successfully installed Toit SDK into", sdkPath)
	return nil
}
