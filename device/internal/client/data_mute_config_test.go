package client

import (
	"testing"
	"time"

	"github.com/wilbowes/EchoMuse/internal/config"
)

// TestSetMutedStopsStreamAndRefusesStart pins the software half of mute:
// muting stops the running stream, and while muted NO caller can start one
// — not the controller's mic_start, not connect()'s automatic wake-stream
// start on a data reconnect. Before this, a data reconnect while muted
// restarted capture against a muted ADC.
func TestSetMutedStopsStreamAndRefusesStart(t *testing.T) {
	mic := newFanoutMic()
	defer mic.close()
	conn, cleanup := dialTestWS(t)
	defer cleanup()

	d := NewDataClient("mute-test", mic, nil)
	d.connMu.Lock()
	d.conn = conn
	d.connMu.Unlock()
	d.StartMic(false)
	if !d.micActive {
		t.Fatal("wake stream did not start")
	}

	d.SetMuted(true)
	if !d.Muted() {
		t.Fatal("Muted() false after SetMuted(true)")
	}
	d.micMu.Lock()
	active := d.micActive
	d.micMu.Unlock()
	if active {
		t.Fatal("mute left the mic stream active")
	}
	// Every start path is refused while muted (controller mic_start,
	// connect()'s auto-start, a lock_mic turn start).
	d.StartMic(false)
	d.StartMic(true)
	d.micMu.Lock()
	active = d.micActive
	d.micMu.Unlock()
	if active {
		t.Fatal("StartMic spawned a stream while muted")
	}
	time.Sleep(20 * time.Millisecond) // let the stopped goroutine drain
}

// TestUnmuteRestartsWakeStreamWithoutGrant is the regression the mute
// callback used to hit on the AFE path: unmute must bring the wake stream
// back (so the on-device detector runs again) but must NOT grant it to the
// controller — the old StartMic-on-unmute turned into a "controller:start"
// grant whenever a stream had survived the mute, streaming live audio
// upstream with no turn to receive it.
func TestUnmuteRestartsWakeStreamWithoutGrant(t *testing.T) {
	mic := newFanoutMic()
	defer mic.close()
	conn, cleanup := dialTestWS(t)
	defer cleanup()

	d := NewDataClient("unmute-test", mic, nil)
	d.connMu.Lock()
	d.conn = conn
	d.connMu.Unlock()

	// Boot-muted device: the flag is set before any stream exists, and the
	// data plane's automatic StartMic(false) on connect is refused.
	d.SetMuted(true)
	d.StartMic(false)
	d.micMu.Lock()
	active := d.micActive
	d.micMu.Unlock()
	if active {
		t.Fatal("stream started while muted")
	}

	// Simulate a stream that slipped in under the flag (race between the
	// mute callback and a reconnect): unmute must tear it down and restart
	// rather than inherit it as "already active".
	d.muted.Store(false)
	d.StartMic(false)
	d.micMu.Lock()
	first := d.micStopCh
	d.micMu.Unlock()
	d.muted.Store(true)

	d.SetMuted(false)
	if d.Muted() {
		t.Fatal("Muted() true after SetMuted(false)")
	}
	d.micMu.Lock()
	active = d.micActive
	second := d.micStopCh
	d.micMu.Unlock()
	if !active {
		t.Fatal("unmute did not restart the wake stream")
	}
	if first == second {
		t.Fatal("unmute inherited the stale stream instead of restarting it")
	}
	d.wakeMu.Lock()
	granted, requestID := d.wakeGranted, d.activeRequestID
	d.wakeMu.Unlock()
	if granted || requestID != "" {
		t.Fatalf("unmute granted the stream (granted=%v request=%q) — audio would leave the device with no turn", granted, requestID)
	}
	d.StopMic()
	time.Sleep(20 * time.Millisecond)
}

// TestUnmuteWithoutConnectionLeavesStartToConnect: no data socket yet means
// nothing to start; the flag is cleared so connect()'s own StartMic works.
func TestUnmuteWithoutConnectionLeavesStartToConnect(t *testing.T) {
	d := NewDataClient("unmute-noconn", newFanoutMic(), nil)
	d.SetMuted(true)
	d.SetMuted(false)
	if d.Muted() || d.micActive {
		t.Fatalf("muted=%v active=%v after unmute without a connection", d.Muted(), d.micActive)
	}
}

// TestGrantMicReplayWindowFollowsConfig verifies wakeReplayFrames from the
// controller config bounds the pre-activation replay: 0 replays only from
// the activation frame onward (so the wake phrase stays out of the
// transcript), the default reproduces the historical 25-frame preroll, and
// the "never bridge a sequence gap" rule survives either way.
func TestGrantMicReplayWindowFollowsConfig(t *testing.T) {
	cfg := config.Get()
	restore := cfg.Snapshot().WakeReplayFrames
	t.Cleanup(func() { cfg.Apply(config.ConfigMessage{WakeReplayFrames: restore}) })

	d := NewDataClient("replay-test", nil, nil)
	for i := 0; i < 30; i++ {
		d.localRing.Push(uint16(i), []byte{byte(i)})
	}

	zero := 0
	cfg.Apply(config.ConfigMessage{WakeReplayFrames: &zero})
	if !d.GrantMic("wake:0", 27, true, time.Now().Add(time.Second)) {
		t.Fatal("grant with zero preroll rejected")
	}
	if len(d.pendingReplay) != 3 || d.pendingReplay[0].Sequence != 27 {
		t.Fatalf("zero-preroll replay = %d frames from seq %d, want 3 from 27",
			len(d.pendingReplay), d.pendingReplay[0].Sequence)
	}

	five := 5
	cfg.Apply(config.ConfigMessage{WakeReplayFrames: &five})
	if !d.GrantMic("wake:5", 27, true, time.Now().Add(time.Second)) {
		t.Fatal("grant with 5-frame preroll rejected")
	}
	if len(d.pendingReplay) != 8 || d.pendingReplay[0].Sequence != 22 {
		t.Fatalf("5-frame replay = %d frames from seq %d, want 8 from 22",
			len(d.pendingReplay), d.pendingReplay[0].Sequence)
	}

	// The historical default needs 25 frames BEFORE the activation: an
	// activation at seq 27 has them (0..26), seq 10 does not.
	dflt := config.DefaultWakeReplayFrames
	cfg.Apply(config.ConfigMessage{WakeReplayFrames: &dflt})
	if !d.GrantMic("wake:25", 27, true, time.Now().Add(time.Second)) {
		t.Fatal("grant with default preroll rejected")
	}
	if len(d.pendingReplay) != 28 || d.pendingReplay[0].Sequence != 2 {
		t.Fatalf("default replay = %d frames from seq %d, want 28 from 2",
			len(d.pendingReplay), d.pendingReplay[0].Sequence)
	}
	if d.GrantMic("wake:short", 10, true, time.Now().Add(time.Second)) {
		t.Fatal("grant accepted without enough preroll history")
	}

	// A sequence gap inside the requested window still refuses the grant.
	gapRing := NewDataClient("replay-gap", nil, nil)
	for _, seq := range []uint16{20, 21, 23, 24, 25} {
		gapRing.localRing.Push(seq, []byte{byte(seq)})
	}
	cfg.Apply(config.ConfigMessage{WakeReplayFrames: &five})
	if gapRing.GrantMic("wake:gap", 25, true, time.Now().Add(time.Second)) {
		t.Fatal("grant bridged a sequence gap")
	}
	cfg.Apply(config.ConfigMessage{WakeReplayFrames: &zero})
	if !gapRing.GrantMic("wake:gap0", 25, true, time.Now().Add(time.Second)) {
		t.Fatal("zero-preroll grant refused although the activation frame is buffered")
	}
}

// TestNoSpeechTimeoutFollowsConfig verifies the deadline is read from the
// controller config (noSpeechTimeoutMs) when no test override is set, and
// that 0 reads as "disabled".
func TestNoSpeechTimeoutFollowsConfig(t *testing.T) {
	cfg := config.Get()
	restore := cfg.Snapshot().NoSpeechTimeoutMs
	t.Cleanup(func() { cfg.Apply(config.ConfigMessage{NoSpeechTimeoutMs: restore}) })
	old := noSpeechTimeoutForTest
	t.Cleanup(func() { noSpeechTimeoutForTest = old })
	noSpeechTimeoutForTest = 0

	if got := effectiveNoSpeechTimeout(); got != 5*time.Second {
		t.Fatalf("default deadline = %v, want 5s", got)
	}
	ms := 8000
	cfg.Apply(config.ConfigMessage{NoSpeechTimeoutMs: &ms})
	if got := effectiveNoSpeechTimeout(); got != 8*time.Second {
		t.Fatalf("configured deadline = %v, want 8s", got)
	}
	ms = 0
	cfg.Apply(config.ConfigMessage{NoSpeechTimeoutMs: &ms})
	if got := effectiveNoSpeechTimeout(); got != 0 {
		t.Fatalf("disabled deadline = %v, want 0", got)
	}
	// The test override still wins over config.
	noSpeechTimeoutForTest = 17 * time.Millisecond
	if got := effectiveNoSpeechTimeout(); got != 17*time.Millisecond {
		t.Fatalf("override = %v", got)
	}
}

// TestNoSpeechTimerDisabledKeepsSilentGrantOpen runs a granted, silent
// stream with noSpeechTimeoutMs=0 and checks no no-speech-timeout sentinel
// is ever sent: end-of-turn is owned upstream for slow-STT setups.
func TestNoSpeechTimerDisabledKeepsSilentGrantOpen(t *testing.T) {
	cfg := config.Get()
	restore := cfg.Snapshot().NoSpeechTimeoutMs
	t.Cleanup(func() { cfg.Apply(config.ConfigMessage{NoSpeechTimeoutMs: restore}) })
	old := noSpeechTimeoutForTest
	t.Cleanup(func() { noSpeechTimeoutForTest = old })
	noSpeechTimeoutForTest = 0
	zero := 0
	cfg.Apply(config.ConfigMessage{NoSpeechTimeoutMs: &zero})

	result := runNoSpeechGrant(t, false)
	if result.sentinelTimeout {
		t.Fatal("timer disabled by config still ended the grant with a no-speech-timeout sentinel")
	}
	if result.micFrames == 0 {
		t.Fatal("granted stream sent no mic frames")
	}
}
