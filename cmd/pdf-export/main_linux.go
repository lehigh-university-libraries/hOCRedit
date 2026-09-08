// pdf-export is the bounded stdin/stdout command served by Scyllaridae.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lehigh-university-libraries/scribe/internal/pdfexport"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 85*time.Second)
	defer cancel()
	stopRead := context.AfterFunc(ctx, func() { _ = os.Stdin.Close() })
	defer stopRead()
	if err := pdfexport.Convert(ctx, os.Stdin, os.Stdout); err != nil {
		// Child diagnostics and document content never reach Scyllaridae logs.
		fmt.Fprintln(os.Stderr, "PDF conversion failed")
		os.Exit(1)
	}
}
