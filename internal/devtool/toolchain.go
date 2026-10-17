package devtool

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CheckNpmCooldown verifies npm's effective configuration as printed by `npm config get
// min-release-age before min-release-age-exclude`: the cooldown is at least minReleaseAgeDays and
// nothing overrides it.
func CheckNpmCooldown(config string) []string {
	values := map[string]string{}
	for _, l := range strings.Split(config, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			values[key] = value
		}
	}
	var problems []string
	age, set := values["min-release-age"]
	if days, err := strconv.Atoi(age); !set {
		problems = append(problems, "min-release-age is not set")
	} else if err != nil || days < minReleaseAgeDays {
		problems = append(problems, fmt.Sprintf("min-release-age is %s, want at least %d", age, minReleaseAgeDays))
	}
	for _, key := range []string{"before", "min-release-age-exclude"} {
		if v := values[key]; v != "" && v != "null" {
			problems = append(problems, key+" is "+v+", want it unset")
		}
	}
	return problems
}

func (c *checker) npmCooldown() stepResult {
	out, err := c.sys.Capture("npm", "--prefix", "frontend", "config", "get", "min-release-age", "before", "min-release-age-exclude")
	if err != nil {
		return fail("npm config get: "+err.Error(), out)
	}
	if problems := CheckNpmCooldown(out); len(problems) > 0 {
		return fail(strings.Join(problems, "; "), "")
	}
	return pass(fmt.Sprintf("min-release-age ≥ %d days, no override", minReleaseAgeDays))
}

// goEnvDefaults are the Go settings the dependency checks rely on: modules come from the proxy whose
// dates the gate checks, every checksum is verified against the checksum database, and nothing is
// fetched insecurely or exempted. Every other setting named here must be unset.
var goEnvDefaults = map[string]string{"GOPROXY": "https://proxy.golang.org,direct", "GOSUMDB": "sum.golang.org"}

var goEnvNames = []string{"GOFLAGS", "GOINSECURE", "GONOPROXY", "GONOSUMDB", "GOPRIVATE", "GOPROXY", "GOSUMDB"}

// CheckGoEnv verifies Go's effective configuration as printed by `go env -json` for goEnvNames.
func CheckGoEnv(env map[string]string) []string {
	var problems []string
	for _, name := range goEnvNames {
		switch want, value := goEnvDefaults[name], env[name]; {
		case value == want:
		case want == "":
			problems = append(problems, name+" is "+value+", want it unset")
		default:
			problems = append(problems, name+" is "+value+", want "+want)
		}
	}
	return problems
}

func (c *checker) goEnv() stepResult {
	out, err := c.sys.Capture("go", append([]string{"env", "-json"}, goEnvNames...)...)
	if err != nil {
		return fail("go env: "+err.Error(), out)
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		return fail("go env: "+err.Error(), out)
	}
	if problems := CheckGoEnv(env); len(problems) > 0 {
		return fail(strings.Join(problems, "; "), "")
	}
	return pass("default module proxy and checksum database, no exemptions")
}
