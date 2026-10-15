package minimax

import (
	"fmt"

	channelconstant "github.com/sh2001sh/new-api/constant"
	gatewaycontract "github.com/sh2001sh/new-api/internal/gateway/contract"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	"github.com/sh2001sh/new-api/types"
)

func GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := info.ChannelBaseUrl
	if baseURL == "" {
		baseURL = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeMiniMax]
	}

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		return fmt.Sprintf("%s/anthropic/v1/messages", baseURL), nil
	default:
		switch info.RelayMode {
		case gatewaycontract.RelayModeChatCompletions:
			return relaycommon.JoinBaseURLPath(baseURL, "/v1/text/chatcompletion_v2"), nil
		case gatewaycontract.RelayModeImagesGenerations:
			return relaycommon.JoinBaseURLPath(baseURL, "/v1/image_generation"), nil
		case gatewaycontract.RelayModeAudioSpeech:
			return relaycommon.JoinBaseURLPath(baseURL, "/v1/t2a_v2"), nil
		default:
			return "", fmt.Errorf("unsupported relay mode: %d", info.RelayMode)
		}
	}
}
