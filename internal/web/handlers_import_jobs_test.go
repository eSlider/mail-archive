package web

import (
	"testing"
	"time"
)

func TestDeleteImportJobRemovesEntryImmediately(t *testing.T) {
	t.Cleanup(resetImportJobsForTest)

	jobID := "job-immediate"
	setImportJob(jobID, &importJob{ID: jobID, Phase: "uploading"})

	deleteImportJob(jobID)

	if _, ok := getImportJob(jobID); ok {
		t.Fatalf("expected import job %q to be removed", jobID)
	}
}

func TestScheduleImportJobCleanupRemovesEntryAfterRetention(t *testing.T) {
	t.Cleanup(resetImportJobsForTest)

	previousRetention := importJobRetention
	importJobRetention = 10 * time.Millisecond
	t.Cleanup(func() { importJobRetention = previousRetention })

	jobID := "job-retained"
	setImportJob(jobID, &importJob{ID: jobID, Phase: "done"})

	scheduleImportJobCleanup(jobID)

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if _, ok := getImportJob(jobID); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected import job %q to be cleaned up", jobID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func resetImportJobsForTest() {
	importJobsMu.Lock()
	defer importJobsMu.Unlock()
	importJobsMap = make(map[string]*importJob)
}
