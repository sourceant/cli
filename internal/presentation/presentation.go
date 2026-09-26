// Package presentation renders what the CLI has to say.
package presentation

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

// Table writes aligned columns, header first.
func Table(out io.Writer, header []string, rows [][]string) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if len(header) > 0 {
		writeRow(writer, header)
	}
	for _, row := range rows {
		writeRow(writer, row)
	}
	_ = writer.Flush()
}

func writeRow(writer io.Writer, cells []string) {
	for i, cell := range cells {
		if i > 0 {
			_, _ = fmt.Fprint(writer, "\t")
		}
		_, _ = fmt.Fprint(writer, cell)
	}
	_, _ = fmt.Fprintln(writer)
}

// Count renders a number with its noun, singular where that reads better.
func Count(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// Since renders how long ago a moment was, in the coarsest unit that still says
// something: a person reading a table wants "3 days", not 4,317 minutes.
func Since(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return Count(int(elapsed.Minutes()), "minute", "minutes") + " ago"
	case elapsed < 24*time.Hour:
		return Count(int(elapsed.Hours()), "hour", "hours") + " ago"
	default:
		return Count(int(elapsed.Hours()/24), "day", "days") + " ago"
	}
}
