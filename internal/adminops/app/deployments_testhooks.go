package app

import "github.com/sh2001sh/new-api/internal/adminops/infra/ionet"

// DeploymentClientForTest exposes the deployment client contract for tests.
type DeploymentClientForTest = ioNetAPI

// SetDeploymentClientFactoriesForTest swaps deployment client factories and returns a restore function.
func SetDeploymentClientFactoriesForTest(publicFactory func(string) DeploymentClientForTest, enterpriseFactory func(string) DeploymentClientForTest) func() {
	previousPublic := newDeploymentPublicClient
	previousEnterprise := newDeploymentEnterpriseClient
	if publicFactory != nil {
		newDeploymentPublicClient = func(apiKey string) ioNetAPI { return publicFactory(apiKey) }
	}
	if enterpriseFactory != nil {
		newDeploymentEnterpriseClient = func(apiKey string) ioNetAPI { return enterpriseFactory(apiKey) }
	}
	return func() {
		newDeploymentPublicClient = previousPublic
		newDeploymentEnterpriseClient = previousEnterprise
	}
}

// ParseConnectionAPIKey parses an optional API key from the raw test-connection request body.
func ParseConnectionAPIKey(rawBody []byte) (string, error) {
	return parseConnectionAPIKey(rawBody)
}

var _ DeploymentClientForTest = (*ionet.Client)(nil)
