package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestLoginSessions 固定「同一时刻只保留一个待扫码会话」这条约束。
//
// 这是浏览器不再堆积的依据：每个待扫码会话都占着一个浏览器活到超时为止，
// 只要新会话没能关掉旧的，进程就会累积。
func TestLoginSessions(t *testing.T) {
	t.Run("开新会话会关掉上一个", func(t *testing.T) {
		var l loginSessions
		closed := 0

		l.start(func() { closed++ })
		assert.Equal(t, 0, closed, "第一个会话不该被关")

		l.start(func() {})
		assert.Equal(t, 1, closed, "开第二个时应关掉第一个")
	})

	t.Run("第一个会话无需关闭任何东西", func(t *testing.T) {
		var l loginSessions
		assert.NotPanics(t, func() { l.start(func() {}) })
	})

	t.Run("会话结束后不会再被关第二次", func(t *testing.T) {
		var l loginSessions
		closed := 0

		seq := l.start(func() { closed++ })
		l.finish(seq)

		l.start(func() {})
		assert.Equal(t, 0, closed, "已结束的会话不该再被关闭")
	})

	t.Run("旧会话的收尾不会顶掉新会话", func(t *testing.T) {
		var l loginSessions
		newClosed := 0

		oldSeq := l.start(func() {})
		l.start(func() { newClosed++ }) // 新会话上位

		// 旧会话此时才走完收尾，它必须认出自己已不是当前会话
		l.finish(oldSeq)

		// 再开一个：如果上一步误清了登记，新会话就永远关不掉了
		l.start(func() {})
		assert.Equal(t, 1, newClosed, "新会话仍应被后来者关闭")
	})

	t.Run("并发开会话时每个序号唯一", func(t *testing.T) {
		var l loginSessions
		const n = 50

		var mu sync.Mutex
		seen := make(map[uint64]bool, n)

		var wg sync.WaitGroup
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				seq := l.start(func() {})
				mu.Lock()
				seen[seq] = true
				mu.Unlock()
			}()
		}
		wg.Wait()

		assert.Len(t, seen, n, "序号必须唯一，否则 finish 会误清别人的登记")
	})

	t.Run("无活跃会话时 waitIfActive 立即返回 false", func(t *testing.T) {
		var l loginSessions
		start := time.Now()
		ok := l.waitIfActive(context.Background(), 2*time.Second)
		assert.False(t, ok)
		assert.True(t, time.Since(start) < 100*time.Millisecond)
	})

	t.Run("活跃会话扫码成功后 waitIfActive 收到通知返回 true", func(t *testing.T) {
		var l loginSessions
		seq := l.start(func() {})

		go func() {
			time.Sleep(50 * time.Millisecond)
			l.notifySuccess(seq)
		}()

		ok := l.waitIfActive(context.Background(), 2*time.Second)
		assert.True(t, ok)
	})

	t.Run("会话未扫码结束时 waitIfActive 提前唤醒返回 false", func(t *testing.T) {
		var l loginSessions
		seq := l.start(func() {})

		go func() {
			time.Sleep(50 * time.Millisecond)
			l.finish(seq)
		}()

		start := time.Now()
		ok := l.waitIfActive(context.Background(), 2*time.Second)
		assert.False(t, ok)
		assert.True(t, time.Since(start) < 500*time.Millisecond, "会话结束后应立刻唤醒，无需等满超时")
	})

	t.Run("waitIfActive 超时返回 false", func(t *testing.T) {
		var l loginSessions
		_ = l.start(func() {})

		start := time.Now()
		ok := l.waitIfActive(context.Background(), 100*time.Millisecond)
		assert.False(t, ok)
		assert.True(t, time.Since(start) >= 90*time.Millisecond)
	})

	t.Run("waitIfActive 响应 ctx 取消", func(t *testing.T) {
		var l loginSessions
		_ = l.start(func() {})

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		ok := l.waitIfActive(ctx, 2*time.Second)
		assert.False(t, ok)
		assert.True(t, time.Since(start) < 500*time.Millisecond)
	})
}
