package runtime

import "time"

const (
	channelCredentialHealthModel = "__channel_credentials__"
	channelCredentialCooldown    = 45 * time.Second
	channelCredentialHealthTTL   = 5 * time.Minute
)

// IsChannelCredentialCooling reports a short runtime hold for credentials
// rejected by an upstream. It never changes the channel's configured status.
func IsChannelCredentialCooling(channelID int) bool {
	state, found := GetChannelHealth(channelID, channelCredentialHealthModel)
	return found && (state.State == ChannelHealthCooling || state.State == ChannelHealthHalfOpen) &&
		(state.CoolingUntil.After(time.Now()) || state.RecoveryProbeUntil.After(time.Now()))
}

// NeedsChannelCredentialRecoveryProbe reports an expired credential hold that
// still requires half-open validation before ordinary traffic resumes.
func NeedsChannelCredentialRecoveryProbe(channelID int) bool {
	state, found := GetChannelHealth(channelID, channelCredentialHealthModel)
	if !found || (state.State != ChannelHealthCooling && state.State != ChannelHealthHalfOpen) {
		return false
	}
	return !state.CoolingUntil.After(time.Now()) && !state.RecoveryProbeUntil.After(time.Now())
}

// RecordChannelCredentialFailure temporarily excludes every model on a
// channel after an explicit upstream authentication or account rejection.
func RecordChannelCredentialFailure(channelID int) {
	if channelID <= 0 {
		return
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, channelCredentialHealthModel), channelCredentialHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		state.ChannelID = channelID
		state.Model = channelCredentialHealthModel
		state.State = ChannelHealthCooling
		state.CoolingUntil = now.Add(channelCredentialCooldown)
		state.RecoveryProbeSuccesses = 0
		state.RecoveryProbeUntil = time.Time{}
		state.RecoveryProbeSlots = 0
		state.LastFailureAt = now
		return state, nil
	})
}

// TryStartChannelCredentialRecoveryProbe permits exactly one request after a
// credential hold expires. It prevents a recovered channel from receiving a
// burst before its upstream account state is known again.
func TryStartChannelCredentialRecoveryProbe(channelID int) bool {
	return tryStartChannelProbe(channelID, channelCredentialHealthModel, 1, true)
}

// RecordChannelCredentialSuccess advances an active credential circuit. Two
// successful real requests are required before normal traffic resumes.
func RecordChannelCredentialSuccess(channelID int) {
	state, found := GetChannelHealth(channelID, channelCredentialHealthModel)
	if !found || (state.State != ChannelHealthCooling && state.State != ChannelHealthHalfOpen) {
		return
	}
	RecordChannelSuccess(channelID, channelCredentialHealthModel, 0)
}
