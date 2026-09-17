package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

func main() {
	// Ctrl-C has to reach the upload in progress, otherwise a long transcription
	// keeps running after the user has asked it to stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCmd(&globalOptions{defaultCacheDir: deepgram.DefaultCacheDir()})

	if err := root.ExecuteContext(ctx); err != nil {
		cliout.New(root.OutOrStdout(), root.ErrOrStderr()).Report(err)
		os.Exit(1)
	}
}
