package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gridctl/gridctl/internal/scenarioverify"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scenarioverify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lane := fs.String("lane", "", "designated lane name")
	indexPath := fs.String("index", "", "path to the reviewed scenario index")
	eventsPath := fs.String("events", "", "path to a completed go test -json capture")
	revision := fs.String("revision", "", "source revision for the summary")
	summaryPath := fs.String("summary", "", "optional machine-readable summary path")
	goStatus := fs.Int("go-status", 0, "exit status of the go test process")
	captureStatus := fs.Int("capture-status", 0, "exit status of the capture/tee command")
	list := fs.Bool("list", false, "print required scenario IDs and focused reproduction commands")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *lane == "" || *indexPath == "" || (!*list && *eventsPath == "") {
		fmt.Fprintln(stderr, "usage: scenarioverify -lane LANE -index INDEX -events CAPTURE")
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	ctx := context.Background()
	idx, err := scenarioverify.LoadIndex(ctx, *indexPath)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if *list {
		required, err := idx.Required(*lane)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 2
		}
		for _, sc := range required {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", sc.ID, sc.Test, scenarioverify.FocusedCommand(sc, *lane))
		}
		return 0
	}
	events, err := os.Open(*eventsPath)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", scenarioverify.ReasonIncompleteEvidence, err)
		return 1
	}
	defer func() { _ = events.Close() }()

	rep, err := scenarioverify.Verify(ctx, idx, events, scenarioverify.VerifyOptions{
		Lane:          *lane,
		Revision:      *revision,
		GoStatus:      *goStatus,
		CaptureStatus: *captureStatus,
		GoStatusSet:   set["go-status"],
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if err := scenarioverify.WriteText(stdout, rep); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *summaryPath != "" {
		sf, err := os.Create(*summaryPath)
		if err != nil {
			fmt.Fprintf(stderr, "summary: %v\n", err)
			return 1
		}
		err = scenarioverify.WriteJSON(sf, rep)
		_ = sf.Close()
		if err != nil {
			fmt.Fprintf(stderr, "summary: %v\n", err)
			return 1
		}
	}
	if rep.Failed() {
		return 1
	}
	return 0
}
