package main

import (
	"testing"
	"time"
)

func TestSelectBestClientPrefersLeastJobs(t *testing.T) {
	now := time.Now()
	c1 := &ClientInfo{Region: "us-east-1", LastSeen: now, ActiveJobs: 2, Valid: true}
	c2 := &ClientInfo{Region: "us-east-1", LastSeen: now, ActiveJobs: 0, Valid: true}

	best := selectBestClient("us-east-1", []*ClientInfo{c1, c2})
	if best != c2 {
		t.Fatalf("expected least busy client, got %v", best)
	}
}

func TestSelectBestClientSkipsUnresponsive(t *testing.T) {
	stale := time.Now().Add(-clientStaleAfter - time.Second)
	c1 := &ClientInfo{Region: "us-east-1", LastSeen: stale, ActiveJobs: 0, Valid: true}
	c2 := &ClientInfo{Region: "us-east-1", LastSeen: time.Now(), ActiveJobs: 1, Valid: true}

	best := selectBestClient("us-east-1", []*ClientInfo{c1, c2})
	if best != c2 {
		t.Fatalf("expected healthy client, got %v", best)
	}
}

func TestSelectBestClientPrefersCloserRegion(t *testing.T) {
	setActiveConfig(&Config{RegionHierarchy: map[string]string{"us-east-1": "usa"}}, "test")

	now := time.Now()
	close := &ClientInfo{Region: "us-east-1", LastSeen: now, ActiveJobs: 0, Valid: true}
	far := &ClientInfo{Region: "usa", LastSeen: now, ActiveJobs: 0, Valid: true}

	best := selectBestClient("us-east-1", []*ClientInfo{far, close})
	if best != close {
		t.Fatalf("expected closest region match, got %v", best)
	}
}
