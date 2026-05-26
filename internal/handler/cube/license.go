package cube

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/scrypt"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"ablecloud.io/ablestack-api/internal/service/security"
)

type LicenseRequest = CubeModel.LicenseRequest
type LicenseResponse = CubeModel.LicenseResponse
type LicenseStatus = CubeModel.LicenseStatus

// LicenseControl godoc
//
//	@Summary		License Control
//	@Description	라이센스 등록 및 상태 확인을 수행합니다.
//	@Tags			CUBE - License
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CubeModel.LicenseRequest	true	"license request"
//	@Success		200	{object}	CubeModel.LicenseResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/license [post]
func LicenseControl(context *gin.Context) {
	var req LicenseRequest
	if err := context.ShouldBindJSON(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	action := normalizeLicenseAction(req.Action)
	if action == "" {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "unsupported action",
		})
		return
	}

	var (
		resp LicenseResponse
		err  error
	)

	switch action {
	case "status":
		resp, err = getLicenseStatus()
	case "register":
		resp, err = registerLicense(req.LicenseContent, req.OriginalFilename)
	}
	if err != nil {
		context.JSON(http.StatusInternalServerError, utils.HTTP500InternalServerError{
			ErrCode: http.StatusInternalServerError,
			Message: err.Error(),
		})
		return
	}

	if resp.Code != 200 {
		context.JSON(http.StatusInternalServerError, resp)
		return
	}
	context.JSON(http.StatusOK, resp)
}

// normalizeLicenseAction은 지원하는 라이선스 액션만 내부 표준 값으로 변환한다.
func normalizeLicenseAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "status":
		return "status"
	case "register", "update", "create":
		return "register"
	default:
		return ""
	}
}

// getLicenseStatus는 현재 노드에 등록된 라이선스 파일을 읽어 상태를 계산한다.
func getLicenseStatus() (LicenseResponse, error) {
	hostUUID, err := readMachineID()
	if err != nil {
		return LicenseResponse{Code: 500, Val: "호스트 UUID를 읽을 수 없습니다."}, nil
	}

	licenseDir := filepath.Join("/usr/share", hostUUID)
	files, err := os.ReadDir(licenseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return LicenseResponse{Code: 404, Val: "등록된 라이센스가 없습니다."}, nil
		}
		return LicenseResponse{}, err
	}

	fileNames := make([]string, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		fileNames = append(fileNames, entry.Name())
	}
	if len(fileNames) == 0 {
		return LicenseResponse{Code: 404, Val: "등록된 라이센스가 없습니다."}, nil
	}
	sort.Strings(fileNames)

	licenseFile := filepath.Join(licenseDir, fileNames[len(fileNames)-1])
	raw, err := os.ReadFile(licenseFile)
	if err != nil {
		return LicenseResponse{}, err
	}

	info, err := decryptLicenseInfo(strings.TrimSpace(string(raw)))
	if err != nil {
		return LicenseResponse{Code: 500, Val: fmt.Sprintf("라이센스 정보를 읽을 수 없습니다: %s", err.Error())}, nil
	}

	if info.Expired == "" || info.Issued == "" {
		return LicenseResponse{Code: 500, Val: "라이센스 정보를 읽을 수 없습니다: 만료일 또는 시작일을 찾을 수 없습니다"}, nil
	}

	expiredDate, err := time.Parse("2006-01-02", info.Expired)
	if err != nil {
		return LicenseResponse{Code: 500, Val: fmt.Sprintf("라이센스 정보를 읽을 수 없습니다: %s", err.Error())}, nil
	}

	status := "active"
	today := time.Now().Truncate(24 * time.Hour)
	if today.After(expiredDate) {
		status = "inactive"
	}

	resp := LicenseStatus{
		Status:   status,
		Expired:  info.Expired,
		Issued:   info.Issued,
		OEM:      info.OEM,
		FilePath: licenseFile,
	}
	return LicenseResponse{Code: 200, Val: resp}, nil
}

// registerLicense는 전달받은 라이선스 내용을 복호화 검증 후 호스트 전용 경로에 저장한다.
func registerLicense(content string, originalFilename string) (LicenseResponse, error) {
	if strings.TrimSpace(content) == "" {
		return LicenseResponse{Code: 400, Val: "유효하지 않은 라이센스 내용입니다."}, nil
	}

	licenseContent, err := decodeLicenseContent(content)
	if err != nil {
		return LicenseResponse{Code: 400, Val: "유효하지 않은 라이센스 내용입니다."}, nil
	}

	hostUUID, err := readMachineID()
	if err != nil {
		return LicenseResponse{Code: 500, Val: "호스트 UUID를 읽을 수 없습니다."}, nil
	}

	info, err := decryptLicenseInfo(licenseContent)
	if err != nil {
		return LicenseResponse{Code: 500, Val: fmt.Sprintf("라이센스 내용을 처리할 수 없습니다: %s", err.Error())}, nil
	}
	if info.Expired == "" || info.Issued == "" {
		return LicenseResponse{Code: 500, Val: "라이센스 내용을 처리할 수 없습니다: 만료일 또는 시작일을 찾을 수 없습니다"}, nil
	}

	licenseDir := filepath.Join("/usr/share", hostUUID)
	if err := os.MkdirAll(licenseDir, 0o700); err != nil {
		return LicenseResponse{}, err
	}

	entries, _ := os.ReadDir(licenseDir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(licenseDir, entry.Name()))
	}

	filename := strings.TrimSpace(originalFilename)
	if filename != "" {
		filename = filepath.Base(filename)
	}
	if strings.TrimSpace(filename) == "" {
		return LicenseResponse{Code: 400, Val: "original_filename required"}, nil
	}
	newPath := filepath.Join(licenseDir, filename)

	if err := os.WriteFile(newPath, []byte(licenseContent), fs.FileMode(0o600)); err != nil {
		return LicenseResponse{}, err
	}
	if err := os.Chmod(newPath, 0o600); err != nil {
		return LicenseResponse{}, err
	}
	if _, _, err := security.EnsureInternalToken(); err != nil {
		return LicenseResponse{}, err
	}

	return LicenseResponse{Code: 200, Val: "라이센스가 성공적으로 등록되었습니다."}, nil
}

// licenseInfo는 복호화된 라이선스의 핵심 필드를 담는 내부 구조체다.
type licenseInfo struct {
	Expired string `json:"expired"`
	Issued  string `json:"issued"`
	OEM     string `json:"oem"`
}

// readMachineID는 현재 노드의 machine-id를 읽는다.
func readMachineID() (string, error) {
	raw, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// decodeLicenseContent는 API 입력으로 받은 base64 문자열을 평문 라이선스 내용으로 복원한다.
func decodeLicenseContent(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// decryptLicenseInfo는 암호화된 라이선스 내용을 복호화해 JSON 구조체로 변환한다.
func decryptLicenseInfo(base64Content string) (licenseInfo, error) {
	const (
		password = "password"
		salt     = "salt"
	)

	key, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 32)
	if err != nil {
		return licenseInfo{}, err
	}
	iv, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 16)
	if err != nil {
		return licenseInfo{}, err
	}

	encryptedBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Content))
	if err != nil {
		return licenseInfo{}, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return licenseInfo{}, err
	}
	if len(encryptedBytes)%aes.BlockSize != 0 {
		return licenseInfo{}, fmt.Errorf("invalid encrypted size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(encryptedBytes))
	mode.CryptBlocks(plain, encryptedBytes)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return licenseInfo{}, err
	}

	var info licenseInfo
	if err := json.Unmarshal(plain, &info); err != nil {
		return licenseInfo{}, err
	}
	return info, nil
}

// pkcs7Unpad는 AES-CBC 복호화 후 남은 PKCS#7 패딩을 제거한다.
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padding size")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, v := range data[len(data)-padLen:] {
		if int(v) != padLen {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-padLen], nil
}
