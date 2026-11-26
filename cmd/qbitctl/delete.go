package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func confirmAndDelete(cmd *cobra.Command, app *App, args []string) error {
	if !tuiTerminal(cmd.ErrOrStderr()) {
		return codedErr(exitBadArgs, "delete requires a terminal for the confirmation prompt or --yes")
	}
	torrents, err := app.ResolveTorrents(args)
	if err != nil {
		return err
	}
	var preview strings.Builder
	preview.WriteString("Permanently delete the files for these torrents?\n")
	hashes := make([]string, len(torrents))
	for i, torrent := range torrents {
		hashes[i] = torrent.Hash
		fmt.Fprintf(&preview, "  %s  %q\n", torrent.Hash, torrent.Name)
	}
	preview.WriteString("Type DELETE to confirm: ")
	if _, err := io.WriteString(cmd.ErrOrStderr(), preview.String()); err != nil {
		return fmt.Errorf("write delete confirmation: %w", err)
	}
	confirmed, err := readDeleteConfirmation(app.Ctx, cmd.InOrStdin())
	if err != nil {
		return err
	}
	if !confirmed {
		cmd.PrintErrln("Deletion canceled.")
		return nil
	}
	return app.StopAndRemoveTorrent(strings.Join(hashes, "|"), true)
}

func readDeleteConfirmation(ctx context.Context, input io.Reader) (bool, error) {
	// The command owns the terminal read until process exit. Select on its
	// context because a terminal read can remain blocked after a signal.
	type confirmation struct {
		accepted bool
		err      error
	}
	read := make(chan confirmation, 1)
	go func() {
		line, err := bufio.NewReaderSize(input, 32).ReadSlice('\n')
		if errors.Is(err, io.EOF) || errors.Is(err, bufio.ErrBufferFull) {
			read <- confirmation{}
			return
		}
		read <- confirmation{
			accepted: string(line) == "DELETE\n" || string(line) == "DELETE\r\n",
			err:      err,
		}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-read:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return result.accepted, result.err
	}
}
