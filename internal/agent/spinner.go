package agent

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"
)

// Spinner shows an animated indicator while waiting for the API.
// The animation stays visible after stopping (no clearing on Windows).
type Spinner struct {
	mu     sync.Mutex
	active bool
	msg    string
	stopCh chan struct{}
	doneCh chan struct{}
}

// spinnerPhrases cycles through friendly status hints so the "thinking..."
// line feels alive rather than static.
var spinnerPhrases = []string{
	"thinking",
	"reading the codebase",
	"weighing options",
	"planning the approach",
	"writing code",
	"checking details",
	"connecting the dots",
}

func NewSpinner(msg string) *Spinner {
	return &Spinner{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		msg:    msg,
	}
}

func (s *Spinner) Start() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		defer close(s.doneCh)
		i := 0
		phraseIdx := rand.Intn(len(spinnerPhrases))
		lastSwap := time.Now()
		for {
			select {
			case <-s.stopCh:
				// Clear the spinner line and move to a fresh line so the next
				// output (assistant message / tool header) starts clean.
				fmt.Fprint(os.Stdout, "\r\033[K")
				return
			default:
				phrase := spinnerPhrases[phraseIdx%len(spinnerPhrases)]
				if time.Since(lastSwap) > 1500*time.Millisecond {
					phraseIdx++
					lastSwap = time.Now()
				}
				fmt.Fprintf(os.Stdout, "\r\033[K  \033[38;5;75m%s\033[0m \033[38;5;241m%s...\033[0m",
					frames[i%len(frames)], phrase)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()
	close(s.stopCh)
	<-s.doneCh
}
