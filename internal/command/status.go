package command

import (
	"fmt"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/model"
)

// StatusReport is the full team dashboard: who's doing what, what's pending.
type StatusReport struct {
	OpenTasks    []*model.Task
	ReviewTasks  []*model.Task
	MergedTasks  []*model.Task
	Pending      []*model.Proposal
	Approved     []*model.Proposal // approved but not yet merged — ready to merge
	RecentMerged []*model.Proposal // proposals whose task is actually merged
}

// Status gathers the full team state into a single report.
func (a *App) Status() (*StatusReport, error) {
	tasks, err := a.Store.ListTasks()
	if err != nil {
		return nil, err
	}
	proposals, err := a.Store.ListProposals()
	if err != nil {
		return nil, err
	}

	// Index task status so we can tell an approved proposal whose task is
	// merged (truly done) from one that's approved but still awaiting merge.
	taskStatus := make(map[string]model.TaskStatus, len(tasks))
	for _, t := range tasks {
		taskStatus[t.ID] = t.Status
	}

	r := &StatusReport{}
	for _, t := range tasks {
		switch t.Status {
		case model.TaskOpen:
			r.OpenTasks = append(r.OpenTasks, t)
		case model.TaskReview:
			r.ReviewTasks = append(r.ReviewTasks, t)
		case model.TaskMerged:
			r.MergedTasks = append(r.MergedTasks, t)
		}
	}
	for _, p := range proposals {
		switch p.State {
		case model.ReviewPending:
			r.Pending = append(r.Pending, p)
		case model.ReviewApproved:
			// Approved ≠ merged. Only call it "merged" once the task actually is;
			// otherwise it's approved-but-awaiting-merge.
			if taskStatus[p.TaskID] == model.TaskMerged {
				r.RecentMerged = append(r.RecentMerged, p)
			} else {
				r.Approved = append(r.Approved, p)
			}
		}
	}
	return r, nil
}

// FormatStatus renders the dashboard as a terminal-friendly string.
func FormatStatus(r *StatusReport) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %d in progress · %d awaiting review · %d approved · %d merged\n\n",
		len(r.OpenTasks), len(r.ReviewTasks), len(r.Approved), len(r.MergedTasks)))

	if len(r.OpenTasks) > 0 {
		b.WriteString("IN PROGRESS:\n")
		for _, t := range r.OpenTasks {
			who := t.Assignee
			if who == "" {
				who = "(unclaimed)"
			}
			b.WriteString(fmt.Sprintf("  %-10s @%-12s [%s] %s\n",
				t.ID, who, t.Agent, t.Intent))
		}
		b.WriteString("\n")
	}

	if len(r.Pending) > 0 {
		b.WriteString("AWAITING REVIEW:\n")
		for _, p := range r.Pending {
			b.WriteString(fmt.Sprintf("  %-10s  task %-10s [%s] %s\n",
				p.ID, p.TaskID, p.Agent, p.Summary))
		}
		b.WriteString("\n")
	}

	if len(r.Approved) > 0 {
		b.WriteString("APPROVED — READY TO MERGE:\n")
		for _, p := range r.Approved {
			b.WriteString(fmt.Sprintf("  %-10s  task %-10s [%s] %s\n",
				p.ID, p.TaskID, p.Agent, p.Summary))
		}
		b.WriteString("\n")
	}

	if len(r.MergedTasks) > 0 {
		b.WriteString(fmt.Sprintf("RECENTLY MERGED: %d task(s)\n", len(r.MergedTasks)))
		limit := 5
		if len(r.MergedTasks) < limit {
			limit = len(r.MergedTasks)
		}
		for _, t := range r.MergedTasks[:limit] {
			b.WriteString(fmt.Sprintf("  %-10s @%-12s %s\n", t.ID, t.Assignee, t.Intent))
		}
		b.WriteString("\n")
	}

	if len(r.OpenTasks) == 0 && len(r.Pending) == 0 && len(r.Approved) == 0 && len(r.MergedTasks) == 0 {
		b.WriteString("  no activity yet\n")
	}

	return b.String()
}
