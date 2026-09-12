package config

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func boolPtr(b bool) *bool        { return &b }
func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }

// ─── env helpers ────────────────────────────────────────────────────────────

func TestEnvInt(t *testing.T) {
	t.Setenv("EM_TEST_ENVINT", "42")
	if got := envInt("EM_TEST_ENVINT", 7); got != 42 {
		t.Errorf("envInt(set) = %d, want 42", got)
	}
	if got := envInt("EM_TEST_ENVINT_UNSET", 7); got != 7 {
		t.Errorf("envInt(unset) = %d, want default 7", got)
	}
	t.Setenv("EM_TEST_ENVINT_BAD", "not-a-number")
	if got := envInt("EM_TEST_ENVINT_BAD", 9); got != 9 {
		t.Errorf("envInt(unparsable) = %d, want default 9", got)
	}
}

func TestEnvFloat(t *testing.T) {
	t.Setenv("EM_TEST_ENVFLOAT", "3.25")
	if got := envFloat("EM_TEST_ENVFLOAT", 1.0); got != 3.25 {
		t.Errorf("envFloat(set) = %v, want 3.25", got)
	}
	if got := envFloat("EM_TEST_ENVFLOAT_UNSET", 1.5); got != 1.5 {
		t.Errorf("envFloat(unset) = %v, want default 1.5", got)
	}
	t.Setenv("EM_TEST_ENVFLOAT_BAD", "nope")
	if got := envFloat("EM_TEST_ENVFLOAT_BAD", 2.0); got != 2.0 {
		t.Errorf("envFloat(unparsable) = %v, want default 2.0", got)
	}
}

func TestEnvBool(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"True", true},
		{"0", false},
		{"false", false},
		{"garbage", false},
	}
	for _, c := range cases {
		t.Setenv("EM_TEST_ENVBOOL", c.val)
		if got := envBool("EM_TEST_ENVBOOL", false); got != c.want {
			t.Errorf("envBool(%q) = %v, want %v", c.val, got, c.want)
		}
	}
	if got := envBool("EM_TEST_ENVBOOL_UNSET", true); got != true {
		t.Errorf("envBool(unset) = %v, want default true", got)
	}
}

func TestEnvStr(t *testing.T) {
	t.Setenv("EM_TEST_ENVSTR", "hello")
	if got := envStr("EM_TEST_ENVSTR", "default"); got != "hello" {
		t.Errorf("envStr(set) = %q, want hello", got)
	}
	if got := envStr("EM_TEST_ENVSTR_UNSET", "default"); got != "default" {
		t.Errorf("envStr(unset) = %q, want default", got)
	}
}

func TestClampAfeMicGainDb(t *testing.T) {
	for _, c := range []struct{ in, want int }{{-1, 0}, {0, 0}, {12, 12}, {24, 24}, {25, 24}} {
		if got := clampAfeMicGainDb(c.in); got != c.want {
			t.Errorf("clampAfeMicGainDb(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ─── Device.Apply ───────────────────────────────────────────────────────────

func TestApplyIgnoresZeroFields(t *testing.T) {
	// A partial config push must never zero out fields it didn't mention —
	// that's the whole point of Apply's "non-zero means set" contract.
	d := &Device{initialised: true, VadThreshold: 0.01, OwwModel: "orig", AfeMicGainDb: 12}
	d.Apply(ConfigMessage{})
	if d.VadThreshold != 0.01 || d.OwwModel != "orig" || d.AfeMicGainDb != 12 {
		t.Fatalf("Apply(empty) changed fields: %+v", d)
	}
}

func TestApplySetsProvidedFields(t *testing.T) {
	d := &Device{initialised: true}
	mg := 12
	stopModel := "stop_v1"
	d.Apply(ConfigMessage{
		VadThreshold:     0.02,
		VadSpeechMs:      250,
		VadSilenceMs:     500,
		OwwModel:         "new_model",
		StopModel:        &stopModel,
		StopThreshold:    0.77,
		BargeInThreshold: 0.3,
		StartupVolume:    64,
		AfeMicGainDb:     &mg,
	})
	if d.VadThreshold != 0.02 {
		t.Errorf("VadThreshold = %v, want 0.02", d.VadThreshold)
	}
	if d.VadSpeechMs != 250 || d.VadSilenceMs != 500 {
		t.Errorf("VAD timing = %v / %v, want 250 / 500", d.VadSpeechMs, d.VadSilenceMs)
	}
	if d.OwwModel != "new_model" {
		t.Errorf("OwwModel = %q, want new_model", d.OwwModel)
	}
	if d.StopModel != "stop_v1" || d.StopThreshold != 0.77 {
		t.Errorf("stop config = %q / %v", d.StopModel, d.StopThreshold)
	}
	if d.BargeInThreshold != 0.3 {
		t.Errorf("BargeInThreshold = %v, want 0.3", d.BargeInThreshold)
	}
	if d.StartupVolume != 64 {
		t.Errorf("StartupVolume = %v, want 64", d.StartupVolume)
	}
	if d.AfeMicGainDb != 12 {
		t.Errorf("AfeMicGainDb = %d, want 12 (clamped-in-range passthrough)", d.AfeMicGainDb)
	}
}

func TestWakeCaptureConfigSupportsFalseZeroFloorAndClampsDuration(t *testing.T) {
	d := &Device{
		initialised: true, SaveWakeCaptures: true,
		WakeCaptureSec: 2, WakeNearMissFloor: 0.2,
	}
	d.Apply(ConfigMessage{
		SaveWakeCaptures: boolPtr(false), WakeCaptureSec: 99,
		WakeNearMissFloor: floatPtr(0),
	})
	snapshot := d.Snapshot()
	if snapshot.SaveWakeCaptures == nil || *snapshot.SaveWakeCaptures {
		t.Fatal("explicit capture disable was lost")
	}
	if snapshot.WakeCaptureSec != 5 || snapshot.WakeNearMissFloor == nil || *snapshot.WakeNearMissFloor != 0 {
		t.Fatalf("capture snapshot = %#v", snapshot)
	}
}

func TestStopCaptureConfigHandlesEnableAndClampsDuration(t *testing.T) {
	d := &Device{
		initialised: true, SaveStopCaptures: true,
		StopCaptureSec: 2, StopNearMissFloor: 0.2,
	}
	d.Apply(ConfigMessage{
		SaveStopCaptures: boolPtr(false), StopCaptureSec: 99,
		StopNearMissFloor: floatPtr(0),
	})
	snapshot := d.Snapshot()
	if snapshot.SaveStopCaptures == nil || *snapshot.SaveStopCaptures {
		t.Fatal("explicit stop capture disable was lost")
	}
	if snapshot.StopCaptureSec != 5 || snapshot.StopNearMissFloor == nil || *snapshot.StopNearMissFloor != 0 {
		t.Fatalf("stop capture snapshot clamp = %#v", snapshot)
	}
	d.Apply(ConfigMessage{SaveStopCaptures: boolPtr(true), StopCaptureSec: 0.01})
	snapshot = d.Snapshot()
	if snapshot.SaveStopCaptures == nil || !*snapshot.SaveStopCaptures {
		t.Fatal("explicit stop capture enable was lost")
	}
	if snapshot.StopCaptureSec != 0.08 {
		t.Fatalf("stop capture floor clamp = %#v", snapshot)
	}
}

func TestStopCaptureDefaultsArePresent(t *testing.T) {
	d := &Device{}
	d.Apply(ConfigMessage{})
	snap := d.Snapshot()
	if snap.SaveStopCaptures == nil {
		t.Fatal("SaveStopCaptures nil after defaults")
	}
	if *snap.SaveStopCaptures != false {
		t.Fatalf("SaveStopCaptures default = %v, want false", *snap.SaveStopCaptures)
	}
	if snap.StopCaptureSec != 2.0 {
		t.Fatalf("StopCaptureSec default = %v, want 2.0", snap.StopCaptureSec)
	}
	if snap.StopNearMissFloor == nil || *snap.StopNearMissFloor != 0.05 {
		t.Fatalf("StopNearMissFloor default = %#v, want 0.05", snap.StopNearMissFloor)
	}
}

func TestSendspinServerSurvivesConfigSnapshot(t *testing.T) {
	d := &Device{initialised: true}
	d.Apply(ConfigMessage{SendspinServer: "ws://ma.local:8927/sendspin"})
	if got := d.Snapshot().SendspinServer; got != "ws://ma.local:8927/sendspin" {
		t.Fatalf("SendspinServer = %q", got)
	}
}

func TestOutputChainDefaultsMatchController(t *testing.T) {
	d := &Device{}
	d.Apply(ConfigMessage{})
	snap := d.Snapshot()
	wantBands := []float64{4.5, 3.0, -0.5, 0.0, 1.5, 1.0, 0.0, 1.5}
	if !equalFloats(snap.EqBands, wantBands) || !*snap.EqLoudness ||
		*snap.BassShelfHz != 125 || *snap.SubsonicHz != 85 ||
		!*snap.BassGuardEnabled || *snap.BassGuardDb != -30 ||
		!*snap.LimiterEnabled || *snap.LimiterThreshold != -1 || *snap.LimiterRelease != 150 {
		t.Fatalf("output chain defaults = %+v, want controller defaults", snap)
	}
}

func TestApplyOutputChainPreservesPartialConfigAndSnapshotDoesNotAliasBands(t *testing.T) {
	d := &Device{}
	d.Apply(ConfigMessage{EqBands: []float64{1, 2}, LimiterEnabled: boolPtr(false), BassGuardDb: floatPtr(0)})
	d.Apply(ConfigMessage{SubsonicHz: floatPtr(60)})
	d.Apply(ConfigMessage{
		EqLoudness: boolPtr(false), BassShelfHz: floatPtr(90),
		BassGuardEnabled: boolPtr(false), LimiterThreshold: floatPtr(-3),
		LimiterRelease: floatPtr(200),
	})
	snap := d.Snapshot()
	if !equalFloats(snap.EqBands, []float64{1, 2}) || *snap.LimiterEnabled ||
		*snap.BassGuardDb != 0 || *snap.SubsonicHz != 60 || *snap.BassShelfHz != 90 {
		t.Fatalf("output chain config = %+v, want merged values", snap)
	}
	if *snap.EqLoudness || *snap.BassGuardEnabled || *snap.LimiterThreshold != -3 || *snap.LimiterRelease != 200 {
		t.Fatalf("output chain overrides = %+v, want the just-applied values", snap)
	}
	snap.EqBands[0] = 99
	if got := d.Snapshot().EqBands[0]; got != 1 {
		t.Fatalf("Snapshot EqBands aliased live field: got %v, want 1", got)
	}
}

func equalFloats(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestApplyClampsAfeMicGainDb(t *testing.T) {
	d := &Device{initialised: true}
	d.Apply(ConfigMessage{AfeMicGainDb: intPtr(100)})
	if d.AfeMicGainDb != 24 {
		t.Fatalf("Apply did not clamp AfeMicGainDb: got %d, want 24", d.AfeMicGainDb)
	}
}

// DuckDb is negative-going, so it's pointer-typed specifically so that a
// legitimate 0dB setting is distinguishable from "field absent" — this is
// the one field in the message that inverts the usual non-zero rule.
func TestApplyDuckDbZeroIsALegitimateValue(t *testing.T) {
	d := &Device{initialised: true, DuckDb: -18}
	d.Apply(ConfigMessage{DuckDb: floatPtr(0)})
	if d.DuckDb != 0 {
		t.Fatalf("Apply(DuckDb=0) left DuckDb = %v, want 0", d.DuckDb)
	}
}

func TestApplyLoadsDefaultsWhenNotInitialised(t *testing.T) {
	d := &Device{}
	d.Apply(ConfigMessage{VadThreshold: 0.5})
	if !d.initialised {
		t.Fatalf("Apply did not mark the device initialised")
	}
	if d.VadThreshold != 0.5 {
		t.Fatalf("explicit field lost after implicit loadDefaults: VadThreshold = %v, want 0.5", d.VadThreshold)
	}
	// A field the message didn't mention must have picked up its default,
	// not been left at the Go zero value.
	if d.OwwModel == "" {
		t.Fatalf("OwwModel is empty; loadDefaults should have populated it")
	}
}

func TestApplyBoolPointerFieldsReplaceDirectly(t *testing.T) {
	d := &Device{initialised: true}
	d.Apply(ConfigMessage{
		BargeInEnabled:  boolPtr(true),
		BleProxyEnabled: boolPtr(true),
	})
	if !d.BargeInEnabled {
		t.Errorf("BargeInEnabled not applied")
	}
	if d.BleProxyEnabled == nil || *d.BleProxyEnabled != true {
		t.Errorf("BleProxyEnabled not applied: %v", d.BleProxyEnabled)
	}
}

// ─── Device.Snapshot ────────────────────────────────────────────────────────

func TestSnapshotDefaultsWhenPointersNil(t *testing.T) {
	d := &Device{initialised: true}
	snap := d.Snapshot()
	if snap.BleProxyEnabled == nil || *snap.BleProxyEnabled != false {
		t.Errorf("Snapshot BleProxyEnabled default = %v, want false", snap.BleProxyEnabled)
	}
}

func TestSnapshotDoesNotAliasLiveFields(t *testing.T) {
	d := &Device{initialised: true, AfeMicGainDb: 12}
	snap := d.Snapshot()

	d.AfeMicGainDb = 20

	if *snap.AfeMicGainDb != 12 {
		t.Errorf("snapshot AfeMicGainDb aliased live field: got %v, want 12", *snap.AfeMicGainDb)
	}
}

// ─── Get ────────────────────────────────────────────────────────────────────

func TestGetIsASingleton(t *testing.T) {
	a := Get()
	b := Get()
	if a != b {
		t.Fatalf("Get() returned different pointers on repeated calls")
	}
}

func TestApplyAndSnapshotAreSafeConcurrently(t *testing.T) {
	d := &Device{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d.Apply(ConfigMessage{VadThreshold: float64(i + j + 1)})
				d.Snapshot()
			}
		}(i)
	}
	wg.Wait()
	if d.Snapshot().VadThreshold <= 0 {
		t.Fatalf("concurrent Apply/Snapshot left an invalid threshold")
	}
}

// TestApplyStopModelAbsentKeepsAndEmptyClears pins the wire contract the
// controller relies on to turn the stop word OFF: an absent "stopModel" key
// leaves the current model alone (partial push), while an explicit "" clears
// it. Decoded from JSON rather than built as a struct so the presence
// distinction is exercised through the real decoder.
func TestApplyStopModelAbsentKeepsAndEmptyClears(t *testing.T) {
	d := &Device{initialised: true, StopModel: "stop_v1"}

	var absent ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","owwThreshold":0.6}`), &absent); err != nil {
		t.Fatal(err)
	}
	d.Apply(absent)
	if d.StopModel != "stop_v1" {
		t.Fatalf("absent stopModel changed the model: %q", d.StopModel)
	}
	if got := d.Snapshot().StopModelName(); got != "stop_v1" {
		t.Fatalf("Snapshot().StopModelName() = %q, want stop_v1", got)
	}

	var cleared ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","stopModel":""}`), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.StopModel == nil || *cleared.StopModel != "" {
		t.Fatalf("explicit empty stopModel decoded as %v, want pointer to \"\"", cleared.StopModel)
	}
	d.Apply(cleared)
	if d.StopModel != "" {
		t.Fatalf("explicit empty stopModel did not clear the model: %q", d.StopModel)
	}
	if got := d.Snapshot().StopModelName(); got != "" {
		t.Fatalf("Snapshot().StopModelName() after clear = %q, want empty", got)
	}
	if (ConfigMessage{}).StopModelName() != "" {
		t.Fatal("nil StopModel must read as empty")
	}
}

// TestNoSpeechTimeoutAndWakeReplayFramesConfig covers the two turn-shaping
// knobs added for slow STT providers: defaults preserve the historical
// behaviour (5s deadline, 25-frame preroll), explicit zero is honoured
// (timer off / replay from the activation frame only), absent keys leave
// the values alone, and out-of-range values are clamped.
func TestNoSpeechTimeoutAndWakeReplayFramesConfig(t *testing.T) {
	d := &Device{}
	d.mu.Lock()
	d.loadDefaults()
	d.mu.Unlock()
	if d.NoSpeechTimeoutMs != DefaultNoSpeechTimeoutMs || d.WakeReplayFrames != DefaultWakeReplayFrames {
		t.Fatalf("defaults = %d ms / %d frames, want %d / %d",
			d.NoSpeechTimeoutMs, d.WakeReplayFrames, DefaultNoSpeechTimeoutMs, DefaultWakeReplayFrames)
	}
	snap := d.Snapshot()
	if snap.NoSpeechTimeout() != 5*time.Second || snap.WakeReplayFrameCount() != 25 {
		t.Fatalf("snapshot defaults = %v / %d", snap.NoSpeechTimeout(), snap.WakeReplayFrameCount())
	}

	var msg ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","noSpeechTimeoutMs":8000,"wakeReplayFrames":0}`), &msg); err != nil {
		t.Fatal(err)
	}
	d.Apply(msg)
	if d.NoSpeechTimeoutMs != 8000 || d.WakeReplayFrames != 0 {
		t.Fatalf("after push = %d ms / %d frames, want 8000 / 0", d.NoSpeechTimeoutMs, d.WakeReplayFrames)
	}
	snap = d.Snapshot()
	if snap.NoSpeechTimeout() != 8*time.Second || snap.WakeReplayFrameCount() != 0 {
		t.Fatalf("snapshot after push = %v / %d", snap.NoSpeechTimeout(), snap.WakeReplayFrameCount())
	}

	// Absent keys: a partial push must not reset either value.
	var partial ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","owwThreshold":0.6}`), &partial); err != nil {
		t.Fatal(err)
	}
	d.Apply(partial)
	if d.NoSpeechTimeoutMs != 8000 || d.WakeReplayFrames != 0 {
		t.Fatalf("partial push changed values: %d ms / %d frames", d.NoSpeechTimeoutMs, d.WakeReplayFrames)
	}

	// Explicit zero timeout disables the timer.
	zero := 0
	d.Apply(ConfigMessage{NoSpeechTimeoutMs: &zero})
	if d.NoSpeechTimeoutMs != 0 || d.Snapshot().NoSpeechTimeout() != 0 {
		t.Fatalf("zero timeout not honoured: %d", d.NoSpeechTimeoutMs)
	}

	// Clamping: negatives read as 0, huge values are capped, replay frames
	// can never exceed what the detector ring can hold.
	neg, huge := -1, 10_000_000
	d.Apply(ConfigMessage{NoSpeechTimeoutMs: &neg, WakeReplayFrames: &huge})
	if d.NoSpeechTimeoutMs != 0 || d.WakeReplayFrames != MaxWakeReplayFrames {
		t.Fatalf("clamp = %d ms / %d frames", d.NoSpeechTimeoutMs, d.WakeReplayFrames)
	}
	d.Apply(ConfigMessage{NoSpeechTimeoutMs: &huge, WakeReplayFrames: &neg})
	if d.NoSpeechTimeoutMs != MaxNoSpeechTimeoutMs || d.WakeReplayFrames != 0 {
		t.Fatalf("clamp = %d ms / %d frames", d.NoSpeechTimeoutMs, d.WakeReplayFrames)
	}

	// Method fallbacks on a message that never carried the fields.
	if (ConfigMessage{}).NoSpeechTimeout() != 5*time.Second || (ConfigMessage{}).WakeReplayFrameCount() != 25 {
		t.Fatal("nil-field accessors must return the defaults")
	}
}

// TestOwwPatienceFramesConfig: the default keeps the single-frame trigger,
// a push sets it, an absent key leaves it alone, and the value is clamped
// to [1, MaxOwwPatienceFrames].
func TestOwwPatienceFramesConfig(t *testing.T) {
	d := &Device{}
	d.mu.Lock()
	d.loadDefaults()
	d.mu.Unlock()
	if d.OwwPatienceFrames != DefaultOwwPatienceFrames || d.Snapshot().OwwPatienceFrames != 1 {
		t.Fatalf("default = %d, want %d", d.OwwPatienceFrames, DefaultOwwPatienceFrames)
	}

	var msg ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","owwPatienceFrames":2}`), &msg); err != nil {
		t.Fatal(err)
	}
	d.Apply(msg)
	if d.OwwPatienceFrames != 2 || d.Snapshot().OwwPatienceFrames != 2 {
		t.Fatalf("after push = %d, want 2", d.OwwPatienceFrames)
	}

	var partial ConfigMessage
	if err := json.Unmarshal([]byte(`{"type":"config","owwThreshold":0.6}`), &partial); err != nil {
		t.Fatal(err)
	}
	d.Apply(partial)
	if d.OwwPatienceFrames != 2 {
		t.Fatalf("partial push changed the value: %d", d.OwwPatienceFrames)
	}

	d.Apply(ConfigMessage{OwwPatienceFrames: 1000})
	if d.OwwPatienceFrames != MaxOwwPatienceFrames {
		t.Fatalf("clamp = %d, want %d", d.OwwPatienceFrames, MaxOwwPatienceFrames)
	}
}
