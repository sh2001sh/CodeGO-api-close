package runtime

import "time"

// RecordChannelSoftFailureForRequest updates shared reliability observations
// without degrading or cooling the route. Auto routing uses it for short-window
// scoring while keeping transient circuit state isolated to one user.
func RecordChannelSoftFailureForRequest(channelID int, model, requestID string, requestTypes ...RequestType) {
	if channelID <= 0 || model == "" {
		return
	}
	lock := channelHealthLock(channelID)
	lock.Lock()
	defer lock.Unlock()

	now := time.Now().UTC()
	_ = getChannelHealthCache().UpdateWithTTL(channelHealthKey(channelID, model, requestTypes...), channelHealthTTL, func(state ChannelHealth, _ bool) (ChannelHealth, error) {
		if requestID != "" && state.LastFailureRequestID == requestID {
			return state, nil
		}
		state.ChannelID = channelID
		state.Model = model
		state.RequestType = normalizedRequestType(requestTypes...)
		state.LastFailureAt = now
		state.LastFailureRequestID = requestID
		recordChannelHealthWindow(&state, now, false)
		return state, nil
	})
}
