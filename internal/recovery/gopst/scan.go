// Package gopst contains the optional deep-recovery probe based on go-pst.
// It is deliberately separate from the fast indexed reader: recovery mode
// walks node candidates and can be substantially slower.
package gopst

import (
	"fmt"
	"os"

	pst "github.com/useti/go-pst/v6/pkg"
)

type Report struct {
	Backend         string         `json:"backend"`
	TriedNodes      int            `json:"tried_nodes"`
	SuccessfulNodes int            `json:"successful_nodes"`
	FailedNodes     int            `json:"failed_nodes"`
	OrphanMessages  int            `json:"orphan_messages"`
	TypeCounts      map[string]int `json:"type_counts,omitempty"`
	ErrorCategories map[string]int `json:"error_categories,omitempty"`
	Errors          []string       `json:"errors,omitempty"`
}

// Scan performs a read-only recovery probe. It counts candidates and errors;
// it does not export or modify the source file.
func Scan(filename string) (Report, error) {
	return ScanWithProgress(filename, nil)
}

// ScanWithProgress performs the recovery probe and reports node progress.
// The callback must be cheap; it is called during the bounded iterator pass.
func ScanWithProgress(filename string, progress func(done, total int)) (Report, error) {
	r := Report{
		Backend:         "go-pst-recovery",
		TypeCounts:      make(map[string]int),
		ErrorCategories: make(map[string]int),
	}
	f, err := os.Open(filename)
	if err != nil {
		return r, fmt.Errorf("open recovery input: %w", err)
	}
	defer f.Close()

	db, err := pst.NewWithRecoveryMode(f)
	if err != nil {
		return r, fmt.Errorf("open recovery parser: %w", err)
	}
	defer db.Cleanup()

	iter, err := db.GetRecoverableMessagesIterator()
	if err != nil {
		return r, fmt.Errorf("create recoverable-node iterator: %w", err)
	}
	recoverablePanic := false
	for {
		ok, panicText := safeNext(iter.Next)
		if panicText != "" {
			r.Errors = append(r.Errors, "recoverable-node iterator panic: "+panicText)
			recoverablePanic = true
			break
		}
		if !ok {
			break
		}
		if progress != nil {
			progress(iter.TriedNodes, iter.Size())
		}
	}
	if progress != nil {
		progress(iter.TriedNodes, iter.Size())
	}
	r.TriedNodes = iter.TriedNodes
	r.SuccessfulNodes = iter.SuccessNodes
	r.FailedNodes = iter.FailedNodes
	r.Errors = append(r.Errors, iter.ErrorMessages...)
	r.TypeCounts = make(map[string]int, len(iter.TypeCounts))
	for kind, count := range iter.TypeCounts {
		r.TypeCounts[string(kind)] = count
	}
	r.ErrorCategories = make(map[string]int, len(iter.ErrorCategoryCounts)+1)
	for kind, count := range iter.ErrorCategoryCounts {
		r.ErrorCategories[kind] = count
	}
	if recoverablePanic {
		r.ErrorCategories["panic"]++
	}

	orphans, orphanErr := db.GetOrphanMessageIterator()
	if orphanErr == nil {
		for {
			ok, panicText := safeNext(orphans.Next)
			if panicText != "" {
				r.Errors = append(r.Errors, "orphan iterator panic: "+panicText)
				r.ErrorCategories["panic"]++
				break
			}
			if !ok {
				break
			}
			r.OrphanMessages++
		}
		if err := orphans.Err(); err != nil {
			r.Errors = append(r.Errors, "orphan scan: "+err.Error())
		}
	} else {
		r.Errors = append(r.Errors, "orphan scan unavailable: "+orphanErr.Error())
	}
	return r, nil
}

// safeNext converts a third-party iterator panic into a normal end-of-scan
// condition. Recovery must be best-effort: one malformed node must not crash
// the inspection process or prevent the report from being written.
func safeNext(next func() bool) (ok bool, panicText string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicText = fmt.Sprint(recovered)
			ok = false
		}
	}()
	return next(), ""
}
