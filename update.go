package main

import (
	"context"
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
)

func runUpdate() error {
	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
	if err != nil {
		return err
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.ParseSlug("BambooTuna/tmuxui"))
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}
	if !found {
		return fmt.Errorf("no release found for this platform")
	}
	if latest.LessOrEqual(version) {
		fmt.Printf("already up to date (%s)\n", version)
		return nil
	}

	fmt.Printf("updating %s -> %s ...\n", version, latest.Version())
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	fmt.Println("updated successfully")
	return nil
}
