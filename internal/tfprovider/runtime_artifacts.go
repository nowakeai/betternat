package tfprovider

import (
	"fmt"
	"sort"
	"strings"
)

type bootstrapArtifacts struct {
	AgentBinaryURL    string
	AgentBinarySHA256 string
	CLIBinaryURL      string
	CLIBinarySHA256   string
}

type runtimeArtifactSet struct {
	AgentBinaryURL    string
	AgentBinarySHA256 string
	CLIBinaryURL      string
	CLIBinarySHA256   string
}

type runtimeArtifactChecksums struct {
	AgentSHA256 string
	CLISHA256   string
}

var supportedRuntimeArtifactChecksums = map[string]map[string]runtimeArtifactChecksums{
	"v0.1.0-alpha.2": {
		"arm64": {
			AgentSHA256: "94c96e730035070f7c4aab291b30e2c14c91d980fc334c6aae28aa4199fef89c",
			CLISHA256:   "003f422c7e44aacc7ed78b3abc3b439e17e73d31b752e8b56b9d5fc5b63527e5",
		},
		"amd64": {
			AgentSHA256: "5c49231100870243f0f31af0703d765f79af5dc8f7248e59f7df36afd48ef5a7",
			CLISHA256:   "0e671ebeb1b2a93fd88a1e2bcdb5c93de01d35313b10ce776ef6dcc49885d200",
		},
	},
	"v0.1.0-alpha.6": {
		"arm64": {
			AgentSHA256: "e5ed963c523a84fb5e496b8a13358662cb80afaf228182cc8e3379741cc8b8c5",
			CLISHA256:   "ff4663fa49daeb42113f015c886c77680472a4c32ad3f29122dd95a703bb4f59",
		},
		"amd64": {
			AgentSHA256: "93ff333bb50d52aca6536eadc8abe8e6f9bf1ec02c56155195f40129525dde56",
			CLISHA256:   "5d5c5cf6a216cab0f12eef3c3c8163c3673f794a427b30fcfb024acd2a87fe66",
		},
	},
	"v0.1.0": {
		"arm64": {
			AgentSHA256: "68ef98b9b55fb7e1eb6874331c91d5755e77d5a27ad8a6af6c0eb742bc0c0305",
			CLISHA256:   "e2608e894adf30097c49ba14e0babf8a365491d5f56f3c6ea1b82b857b39ce1d",
		},
		"amd64": {
			AgentSHA256: "1443bb7c069d5674238d95ebae6656e0931df296d2067f38caa2b6fbca8970c5",
			CLISHA256:   "9118b3e620a5eed0cb5e551faf5293e2b6ad2f9856cdf9d834bcdb675b959946",
		},
	},
	"v0.2.0": {
		"arm64": {
			AgentSHA256: "504e42b39d262ef6b0518a7b62628ddaabcd97ff457f7d9bf477cd9f72035d86",
			CLISHA256:   "e5f07e12dcf31dfba3073d51b942debf2334f5493eb456617a3e0c8e6eda3cf8",
		},
		"amd64": {
			AgentSHA256: "df3207e43eacdf949bab1eda0ec73a1f0f8bea703190c0bc3c852da472020f79",
			CLISHA256:   "70903f738a19fc77045d29a67dde7f595069c02523ff9f6979b44156cb586535",
		},
	},
}

func runtimeArtifacts(version string, osName string, arch string) (runtimeArtifactSet, error) {
	if !strings.HasPrefix(version, "v") {
		return runtimeArtifactSet{}, fmt.Errorf("betternat_version must start with v, got %q", version)
	}
	if osName != "linux" {
		return runtimeArtifactSet{}, fmt.Errorf("unsupported runtime artifact os %q; supported os: linux", osName)
	}
	byArch, ok := supportedRuntimeArtifactChecksums[version]
	if !ok {
		return runtimeArtifactSet{}, fmt.Errorf("unsupported betternat_version %q; supported versions: %s", version, strings.Join(sortedRuntimeVersions(), ", "))
	}
	checksums, ok := byArch[arch]
	if !ok {
		return runtimeArtifactSet{}, fmt.Errorf("unsupported runtime artifact architecture %q for betternat_version %q", arch, version)
	}
	releaseBase := "https://github.com/nowakeai/betternat/releases/download/" + version
	return runtimeArtifactSet{
		AgentBinaryURL:    releaseBase + "/betternat-agent_" + version + "_" + osName + "_" + arch,
		AgentBinarySHA256: checksums.AgentSHA256,
		CLIBinaryURL:      releaseBase + "/betternat_" + version + "_" + osName + "_" + arch,
		CLIBinarySHA256:   checksums.CLISHA256,
	}, nil
}

func sortedRuntimeVersions() []string {
	versions := make([]string, 0, len(supportedRuntimeArtifactChecksums))
	for version := range supportedRuntimeArtifactChecksums {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

func runtimeArchForInstanceType(instanceType string) string {
	family := strings.Split(instanceType, ".")[0]
	if family == "a1" || strings.HasSuffix(family, "g") || strings.HasSuffix(family, "gd") || strings.HasSuffix(family, "gn") || strings.HasSuffix(family, "gen") {
		return "arm64"
	}
	return "amd64"
}
