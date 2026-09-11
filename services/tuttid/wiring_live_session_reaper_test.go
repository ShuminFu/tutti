package main

import (
	"testing"
	"time"
)

// 圆点熄灭的节奏由这两个值决定：闲 5 分钟、每分钟扫一次 —— 上游默认的
// 30 分钟对「会话还在不在用」这个问题来说等于没有信号。
func TestLiveSessionReaperDefaultsAreTightEnoughForPresence(t *testing.T) {
	if got := tuttiLiveSessionIdleAfter(); got != 5*time.Minute {
		t.Fatalf("idle after = %v, want 5m", got)
	}
	if got := tuttiLiveSessionSweepInterval(); got != time.Minute {
		t.Fatalf("sweep interval = %v, want 1m", got)
	}
}

// 环境变量只为调试与真机验证留口：非法值必须退回默认，绝不让一个手滑的
// "0" 或 "abc" 把回收器关掉（IdleAfter<=0 在上游等于整个不起回收协程）。
func TestLiveSessionReaperEnvOverrideFallsBackOnGarbage(t *testing.T) {
	const name = "TUTTI_LIVE_SESSION_IDLE_MINUTES"
	for _, tc := range []struct {
		raw  string
		want time.Duration
	}{
		{"2", 2 * time.Minute},
		{" 0.5 ", 30 * time.Second},
		{"0", 5 * time.Minute},
		{"-3", 5 * time.Minute},
		{"abc", 5 * time.Minute},
		{"", 5 * time.Minute},
	} {
		t.Setenv(name, tc.raw)
		if got := tuttiLiveSessionIdleAfter(); got != tc.want {
			t.Fatalf("%q -> %v, want %v", tc.raw, got, tc.want)
		}
	}
}
