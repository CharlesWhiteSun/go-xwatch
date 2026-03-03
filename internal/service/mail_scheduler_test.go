package service

import (
	"testing"
	"time"
)

func TestMailHeartbeatInterval(t *testing.T) {
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "5")
	if got := mailHeartbeatInterval(); got != 5*time.Second {
		t.Fatalf("heartbeat interval = %s, want 5s", got)
	}
}

func TestMailHeartbeatIntervalInvalid(t *testing.T) {
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "abc")
	if got := mailHeartbeatInterval(); got != 0 {
		t.Fatalf("heartbeat interval for invalid env should be 0, got %s", got)
	}
	t.Setenv("XWATCH_MAIL_HEARTBEAT_SEC", "0")
	if got := mailHeartbeatInterval(); got != 0 {
		t.Fatalf("heartbeat interval for zero should be 0, got %s", got)
	}
}
