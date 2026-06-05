package cube

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
)

const (
	scvmCloudInitISOPath = "/var/lib/libvirt/images/scvm-cloudinit.iso"

	scvmCloudInitPNNIC = "enp0s21"
	scvmCloudInitCNNIC = "enp0s22"
)

// CreateSCVMCloudInit godoc
//
//	@Summary		Create SCVM Cloud-Init ISO
//	@Description	cluster.json과 현재 노드 정보를 기준으로 입력값 없이 SCVM cloud-init ISO를 생성합니다.
//	@Tags			Cube-SCVM
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	CubeModel.GenCloudInitResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/cloudinit/scvm/generate [post]
func CreateSCVMCloudInit(context *gin.Context) {
	cfg, err := loadClusterConfigSection()
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: "failed to read cluster.json",
		})
		return
	}

	req, err := buildCreateSCVMCloudInitRequest(cfg)
	if err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if err := os.MkdirAll(resolveAbleStackVMConfigDir("scvm"), 0755); err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}

	resp := runGenCloudInit(req, cfg)
	if resp.Code == http.StatusOK {
		resp.Message = "scvm cloudinit iso create success"
	}
	context.JSON(statusCodeFromGenCloudInitResponse(resp), resp)
}

func buildCreateSCVMCloudInitRequest(cfg *CubeModel.ClusterConfigSection) (GenCloudInitRequest, error) {
	if cfg == nil {
		return GenCloudInitRequest{}, fmt.Errorf("cluster config not found")
	}
	if !isGenCloudInitHCI(cfg) {
		return GenCloudInitRequest{}, fmt.Errorf("scvm cloudinit requires hci cluster type")
	}

	host, err := findSelfHost(cfg)
	if err != nil {
		return GenCloudInitRequest{}, err
	}

	index := strings.TrimSpace(host.Index)
	if index == "" {
		return GenCloudInitRequest{}, fmt.Errorf("hosts[].index required for self host")
	}

	prefix, err := parseCloudInitPrefix(cfg.MngtNic.CIDR)
	if err != nil {
		return GenCloudInitRequest{}, err
	}

	req := GenCloudInitRequest{
		Type:       "scvm",
		ISOPath:    scvmCloudInitISOPath,
		Hostname:   "scvm" + index,
		PubKey:     cloudInitPubKeyPath,
		PrivKey:    cloudInitPrivKeyPath,
		Hosts:      cloudInitHostsPath,
		MgmtNIC:    cloudInitMgmtNIC,
		MgmtIP:     strings.TrimSpace(host.ScvmMngt),
		MgmtPrefix: prefix,
		MgmtGW:     strings.TrimSpace(cfg.MngtNic.GW),
		DNS:        strings.TrimSpace(cfg.MngtNic.DNS),
		PNNIC:      scvmCloudInitPNNIC,
		PNIP:       strings.TrimSpace(host.Scvm),
		PNPrefix:   prefix,
		CNNIC:      scvmCloudInitCNNIC,
		CNIP:       strings.TrimSpace(host.ScvmCn),
		CNPrefix:   prefix,
	}
	if err := normalizeGenCloudInitRequest(&req); err != nil {
		return GenCloudInitRequest{}, err
	}
	return req, nil
}
