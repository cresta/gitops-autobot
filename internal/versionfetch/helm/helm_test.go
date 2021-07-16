package helm

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const testChartDeps = `apiVersion: v2
name: example-chart-yaml
description: An umbrella chart
icon: https://cresta.com/icons/icon-48x48.png

type: application

version: 0.4.6

appVersion: v0.16.0

dependencies:
  - name: cert-manager
    repository: https://charts.jetstack.io
    version: "v1.1.0"
maintainers:
- name: some-team
`

const testHelmReleaseFlux = `apiVersion: helm.fluxcd.io/v1
kind: HelmRelease
metadata:
  name: gitdb
  namespace: gitdb
spec:
  releaseName: gitdb
  chart:
    # gitops-autobot: changer=helm versionConstraint=1.x.x
    repository: https://cresta.github.io/gitdb/
    name: gitdb
    version: 0.1.25
  values:
    image:
      tag: master-gh.241-a9aef22
    ingress:
      enabled: true
      hosts:
        - host: test.example.com
          paths:
            - /`

func TestParse(t *testing.T) {
	ret, err := ParseHelmReleaseYAML(strings.Split(testHelmReleaseFlux, "\n"))
	require.NoError(t, err)
	require.Equal(t, 1, len(ret))
	require.Equal(t, LineHelmChange{
		upgradeInfo: helmUpgradeInfo{
			Repository:        "https://cresta.github.io/gitdb/",
			ChartName:         "gitdb",
			CurrentVersion:    "0.1.25",
			VersionConstraint: "1.x.x",
		},
		currentVersionLine:       "    version: 0.1.25",
		currentVersionLineNumber: 11,
	}, *ret[0])
}
