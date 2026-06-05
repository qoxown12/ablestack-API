package cube

import (
	stdctx "context"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	utils "ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

var _ = utils.TypeVersion{}

const (
	versionMoldHelpTextPath       = "/usr/share/cloudstack-common/scripts/installer/cloudstack-help-text"
	versionKernelCommand          = "uname"
	versionCockpitBridgeCommand   = "cockpit-bridge"
	versionGlueCommand            = "glue"
	versionAbleStackPackageCmd    = "aspkg"
	versionCommandTimeout         = 5 * time.Second
	versionAbleStackPackageFilter = "ablestack"
)

// Version godoc
//
//	@Summary		Show Versions of CUBE
//	@Description	OS, Kernel, Cockpit, Mold, ABLESTACK 패키지, Glue 버전을 보여줍니다. Glue 버전은 ablestack-hci/ablestack-hci-filesystem에서만 포함합니다.
//	@Tags			CubeModel
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	utils.TypeVersion
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/version [get]
//
// Version은 현재 OS/Kernel/Cockpit/Mold/ABLESTACK 패키지/Glue 버전과 디버그 여부를 반환한다.
func Version(context *gin.Context) {
	dat := CubeModel.Cube().GetVersion()
	dat.Debug = gin.IsDebugging()
	dat.OSVersion = readVersionValue(versionUpdateOSReleasePath, "PRETTY_NAME")
	dat.KernelVersion = readCommandSingleLine(versionKernelCommand, "-r")
	dat.CockpitVersion = readCockpitBridgeVersion()
	dat.MoldVersion = readVersionValue(versionMoldHelpTextPath, "ACS_VERSION")
	dat.AbleStackPackageVersions = readAbleStackPackageVersions()
	if isHCITarget(currentVersionClusterType()) {
		dat.GlueVersion = readGlueVersion()
	}
	context.IndentedJSON(http.StatusOK, dat)
} // @name Version

func readVersionValue(path string, key string) string {
	values, err := parseVersionUpdateKeyValues(path)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(values[key]), "N/A")
}

func currentVersionClusterType() string {
	root, err := loadClusterJSONRoot()
	if err != nil {
		return ""
	}
	return extractClusterType(root)
}

func readGlueVersion() string {
	value, err := runVersionCommand(versionGlueCommand, "version")
	if value != "" {
		return strings.Join(strings.Fields(value), " ")
	}
	if err != nil {
		return "N/A"
	}
	return "N/A"
}

func readCommandSingleLine(command string, args ...string) string {
	value, err := runVersionCommand(command, args...)
	if value != "" {
		return strings.Join(strings.Fields(value), " ")
	}
	if err != nil {
		return "N/A"
	}
	return "N/A"
}

func readCockpitBridgeVersion() string {
	value, err := runVersionCommand(versionCockpitBridgeCommand, "--version")
	if value == "" && err != nil {
		return "N/A"
	}
	for _, line := range strings.Split(value, "\n") {
		key, rawVersion, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), "Version") {
			version := strings.TrimSpace(rawVersion)
			if fields := strings.Fields(version); len(fields) > 0 {
				return fields[0]
			}
			return "N/A"
		}
	}
	return readFirstNonEmptyLine(value)
}

func readAbleStackPackageVersions() []string {
	value, err := runVersionCommand(versionAbleStackPackageCmd, "-qa")
	if value == "" && err != nil {
		return nil
	}

	var packages []string
	for _, line := range strings.Split(value, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(name), versionAbleStackPackageFilter) {
			packages = append(packages, name)
		}
	}
	sort.Strings(packages)
	return packages
}

func readFirstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "N/A"
}

func runVersionCommand(command string, args ...string) (string, error) {
	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), versionCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
