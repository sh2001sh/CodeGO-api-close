package app

import (
	"github.com/gin-gonic/gin"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
)

func reserveLegacyCandidateProbe(c *gin.Context, channel *gatewayschema.Channel, modelName string, mode routePoolProbeMode) bool {
	if channel == nil {
		return false
	}
	requestType := gatewayruntime.RequestTypeFromContext(c)
	channelHealth, channelFound := routePoolChannelHealth(c, channel.Id, modelName, requestType)
	domain := gatewayruntime.ChannelFaultDomain(channel.Type, channel.GetBaseURL())
	domainHealth, domainFound := routePoolFaultDomainHealth(c, domain, modelName, requestType)
	channelProbe := channelFound && (channelHealth.State == gatewayruntime.ChannelHealthCooling || channelHealth.State == gatewayruntime.ChannelHealthHalfOpen)
	domainProbe := domainFound && (domainHealth.State == gatewayruntime.ChannelHealthCooling || domainHealth.State == gatewayruntime.ChannelHealthHalfOpen)
	channelReady := !channelProbe || tryStartRoutePoolChannelProbe(c, channel.Id, modelName, mode, requestType)
	domainReady := !domainProbe || tryStartRoutePoolDomainProbe(c, domain, modelName, mode, requestType)
	if channelReady && domainReady {
		return true
	}
	if channelReady && channelProbe {
		releaseRoutePoolChannelProbe(c, channel.Id, modelName, requestType)
	}
	if domainReady && domainProbe {
		releaseRoutePoolDomainProbe(c, domain, modelName, requestType)
	}
	return false
}
