package cube

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ablecloud.io/ablestack-api/internal/infra/utils"
	CubeModel "ablecloud.io/ablestack-api/internal/model/cube"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
)

type SSHKeyRequest = CubeModel.SSHKeyRequest
type SSHKeyResponse = CubeModel.SSHKeyResponse
type SSHKeyResult = CubeModel.SSHKeyResult
type SSHKeyFileInfo = CubeModel.SSHKeyFileInfo

const (
	sshKeyDefaultDir             = "/root/.ssh"
	sshKeyPrivateName            = "id_rsa"
	sshKeyPublicName             = "id_rsa.pub"
	sshKeyAuthorizedName         = "authorized_keys"
	sshKeyDefaultBits            = 4096
	sshKeyMinBits                = 2048
	sshKeyMaxBits                = 8192
	sshKeyFileMaxBytes           = 256 * 1024
	sshKeyBundleMaxBytes         = 2 * 1024 * 1024
	sshKeyDefaultRetName         = "SSH Key"
	sshKeyBundleUploadField      = "file"
	sshKeyBundleUploadAlias      = "ssh_key"
	sshKeyBundleUploadAliasAlt   = "bundle"
	sshKeyBundleContentType      = "application/octet-stream"
	sshKeyDownloadDisposition    = `attachment; filename="%s"`
	sshKeyBundleFilenameBytes    = 8
	sshKeyBundleNonceBytes       = 12
	sshKeyBundleSecretEnv        = "ABLESTACK_SSH_KEY_BUNDLE_SECRET"
	sshKeyBundleDefaultSecret    = "ablestack-api-ssh-key-bundle-default-secret-v1"
	sshKeyEncryptionContext      = "ablestack-api:ssh-key-bundle:v1"
	sshKeyBundleFilenameTemplate = "%s.dat"
)

var (
	sshKeyBundleMagic = []byte{0x41, 0x53, 0x4b, 0x31}

	sshKeyFileModes = map[string]os.FileMode{
		sshKeyPrivateName:    0o600,
		sshKeyPublicName:     0o644,
		sshKeyAuthorizedName: 0o600,
	}
)

// SSHKey godoc
//
//	@Summary		SSH Key Management
//	@Description	/root/.ssh/id_rsa, id_rsa.pub, authorized_keys 파일을 생성하거나 암호화된 단일 파일로 다운로드/업로드합니다.
//	@Tags			Cube-SSH
//	@Accept			json
//	@Accept			multipart/form-data
//	@Produce		json
//	@Produce		application/octet-stream
//	@Param			body	body		CubeModel.SSHKeyRequest	false	"ssh key request"
//	@Success		200	{object}	CubeModel.SSHKeyResponse
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		409	{object}	CubeModel.SSHKeyResponse
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/cube/ssh/key [post]
func SSHKey(context *gin.Context) {
	req, err := bindSSHKeyRequest(context)
	if err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "invalid request",
		})
		return
	}

	if err := normalizeSSHKeyRequest(&req); err != nil {
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	switch req.Action {
	case "generate":
		resp, err := generateSSHKeyFiles(req)
		if err != nil {
			resp = sshKeyError(req.Action, http.StatusInternalServerError, err.Error())
		}
		context.JSON(statusCodeFromSSHKeyResponse(resp), resp)
	case "download":
		bundle, filename, err := buildEncryptedSSHKeyBundle()
		if err != nil {
			resp := sshKeyError(req.Action, http.StatusInternalServerError, err.Error())
			context.JSON(statusCodeFromSSHKeyResponse(resp), resp)
			return
		}
		context.Header("Content-Disposition", fmt.Sprintf(sshKeyDownloadDisposition, filename))
		context.Header("X-Content-Type-Options", "nosniff")
		context.Data(http.StatusOK, sshKeyBundleContentType, bundle)
	case "upload":
		resp, err := uploadSSHKeyBundle(context, sshKeyShouldOverwrite(req.Overwrite))
		if err != nil {
			resp = sshKeyError(req.Action, http.StatusInternalServerError, err.Error())
		}
		context.JSON(statusCodeFromSSHKeyResponse(resp), resp)
	default:
		context.JSON(http.StatusBadRequest, utils.HTTP400BadRequest{
			ErrCode: http.StatusBadRequest,
			Message: "unsupported action",
		})
	}
}

func bindSSHKeyRequest(context *gin.Context) (SSHKeyRequest, error) {
	var req SSHKeyRequest
	contentType := strings.ToLower(context.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req.Action = firstNonEmpty(context.PostForm("action"), context.Query("action"))
		overwrite, err := parseSSHKeyOptionalBool(firstNonEmpty(context.PostForm("overwrite"), context.Query("overwrite")))
		if err != nil {
			return req, err
		}
		req.Overwrite = overwrite
		bits, err := parseSSHKeyInt(firstNonEmpty(context.PostForm("bits"), context.Query("bits")))
		if err != nil {
			return req, err
		}
		req.Bits = bits
		return req, nil
	}

	if context.Request == nil || context.Request.Body == nil || context.Request.ContentLength == 0 {
		req.Action = context.Query("action")
		overwrite, err := parseSSHKeyOptionalBool(context.Query("overwrite"))
		if err != nil {
			return req, err
		}
		req.Overwrite = overwrite
		bits, err := parseSSHKeyInt(context.Query("bits"))
		if err != nil {
			return req, err
		}
		req.Bits = bits
		return req, nil
	}

	if err := context.ShouldBindJSON(&req); err != nil {
		return req, err
	}
	if strings.TrimSpace(req.Action) == "" {
		req.Action = context.Query("action")
	}
	return req, nil
}

func parseSSHKeyOptionalBool(value string) (*bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("invalid boolean value: %s", value)
	}
	return &parsed, nil
}

func parseSSHKeyInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value: %s", value)
	}
	return parsed, nil
}

func normalizeSSHKeyRequest(req *SSHKeyRequest) error {
	if req == nil {
		return fmt.Errorf("request required")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "generate", "create":
		req.Action = "generate"
	case "download", "export":
		req.Action = "download"
	case "upload", "import":
		req.Action = "upload"
	default:
		return fmt.Errorf("unsupported action")
	}
	if req.Bits == 0 {
		req.Bits = sshKeyDefaultBits
	}
	if req.Bits < sshKeyMinBits || req.Bits > sshKeyMaxBits {
		return fmt.Errorf("bits must be between %d and %d", sshKeyMinBits, sshKeyMaxBits)
	}
	return nil
}

func sshKeyShouldOverwrite(overwrite *bool) bool {
	return overwrite == nil || *overwrite
}

func generateSSHKeyFiles(req SSHKeyRequest) (SSHKeyResponse, error) {
	if err := ensureSSHKeyDirectory(); err != nil {
		return SSHKeyResponse{}, err
	}
	if !sshKeyShouldOverwrite(req.Overwrite) {
		if existing := existingSSHKeyFiles(); len(existing) > 0 {
			message := fmt.Sprintf("ssh key files already exist: %s", strings.Join(existing, ", "))
			return sshKeyResponse(req.Action, http.StatusConflict, message, sshKeyResult(message, "")), nil
		}
	}

	privateKey, publicKey, err := generateRSASSHKeyPair(req.Bits)
	if err != nil {
		return SSHKeyResponse{}, err
	}
	files := map[string][]byte{
		sshKeyPrivateName:    privateKey,
		sshKeyPublicName:     publicKey,
		sshKeyAuthorizedName: publicKey,
	}
	if err := writeSSHKeyFiles(files); err != nil {
		return SSHKeyResponse{}, err
	}

	message := "ssh key files generated"
	return sshKeyResponse(req.Action, http.StatusOK, message, sshKeyResult(message, "")), nil
}

func generateRSASSHKeyPair(bits int) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, nil, err
	}
	privateBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	privateKey := pem.EncodeToMemory(privateBlock)
	if len(privateKey) == 0 {
		return nil, nil, fmt.Errorf("failed to encode private key")
	}
	public, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, ssh.MarshalAuthorizedKey(public), nil
}

func buildEncryptedSSHKeyBundle() ([]byte, string, error) {
	paths := sshKeyPaths()
	privateKey, err := readSSHKeyFileForBundle(paths[sshKeyPrivateName], sshKeyPrivateName)
	if err != nil {
		return nil, "", err
	}
	publicKey, err := readSSHKeyFileForBundle(paths[sshKeyPublicName], sshKeyPublicName)
	if err != nil {
		return nil, "", err
	}
	zipPayload, err := buildSSHKeyZip(map[string][]byte{
		sshKeyPrivateName: privateKey,
		sshKeyPublicName:  publicKey,
	})
	if err != nil {
		return nil, "", err
	}
	encrypted, err := encryptSSHKeyBundle(zipPayload)
	if err != nil {
		return nil, "", err
	}
	filename, err := randomSSHKeyBundleFilename()
	if err != nil {
		return nil, "", err
	}
	return encrypted, filename, nil
}

func readSSHKeyFileForBundle(path string, name string) ([]byte, error) {
	if err := requireRegularFile(path, fmt.Sprintf("%s not found", path)); err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	if len(content) > sshKeyFileMaxBytes {
		return nil, fmt.Errorf("%s is too large", name)
	}
	return content, nil
}

func buildSSHKeyZip(files map[string][]byte) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	writer := zip.NewWriter(buf)
	for _, name := range []string{sshKeyPrivateName, sshKeyPublicName} {
		content := files[name]
		if len(content) == 0 {
			_ = writer.Close()
			return nil, fmt.Errorf("%s content required", name)
		}
		header := &zip.FileHeader{
			Name:   name,
			Method: zip.Deflate,
		}
		header.SetMode(sshKeyFileModes[name])
		fileWriter, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := fileWriter.Write(content); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encryptSSHKeyBundle(plain []byte) ([]byte, error) {
	key, err := sshKeyEncryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, sshKeyBundleNonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, []byte(sshKeyEncryptionContext))
	out := make([]byte, 0, len(sshKeyBundleMagic)+len(nonce)+len(ciphertext))
	out = append(out, sshKeyBundleMagic...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func decryptSSHKeyBundle(encrypted []byte) ([]byte, error) {
	if len(encrypted) <= len(sshKeyBundleMagic)+sshKeyBundleNonceBytes {
		return nil, fmt.Errorf("invalid ssh key file")
	}
	if !bytes.Equal(encrypted[:len(sshKeyBundleMagic)], sshKeyBundleMagic) {
		return nil, fmt.Errorf("invalid ssh key file")
	}
	key, err := sshKeyEncryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceStart := len(sshKeyBundleMagic)
	nonceEnd := nonceStart + sshKeyBundleNonceBytes
	nonce := encrypted[nonceStart:nonceEnd]
	ciphertext := encrypted[nonceEnd:]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(sshKeyEncryptionContext))
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt ssh key file")
	}
	return plain, nil
}

func sshKeyEncryptionKey() ([]byte, error) {
	secret := strings.TrimSpace(os.Getenv(sshKeyBundleSecretEnv))
	if secret == "" {
		secret = sshKeyBundleDefaultSecret
	}
	sum := sha256.Sum256([]byte(sshKeyEncryptionContext + "\x00" + secret))
	return sum[:], nil
}

func randomSSHKeyBundleFilename() (string, error) {
	raw := make([]byte, sshKeyBundleFilenameBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf(sshKeyBundleFilenameTemplate, hex.EncodeToString(raw)), nil
}

func uploadSSHKeyBundle(context *gin.Context, overwrite bool) (SSHKeyResponse, error) {
	encrypted, err := readSSHKeyBundleUpload(context)
	if err != nil {
		return sshKeyError("upload", http.StatusBadRequest, err.Error()), nil
	}
	plain, err := decryptSSHKeyBundle(encrypted)
	if err != nil {
		return sshKeyError("upload", http.StatusBadRequest, err.Error()), nil
	}
	files, err := readSSHKeyFilesFromZip(plain)
	if err != nil {
		return sshKeyError("upload", http.StatusBadRequest, err.Error()), nil
	}
	if err := ensureSSHKeyDirectory(); err != nil {
		return SSHKeyResponse{}, err
	}
	if !overwrite {
		if existing := existingSSHKeyFiles(); len(existing) > 0 {
			message := fmt.Sprintf("ssh key files already exist: %s", strings.Join(existing, ", "))
			return sshKeyResponse("upload", http.StatusConflict, message, sshKeyResult(message, "")), nil
		}
	}
	if err := writeSSHKeyFiles(files); err != nil {
		return SSHKeyResponse{}, err
	}

	message := "ssh key files uploaded"
	return sshKeyResponse("upload", http.StatusOK, message, sshKeyResult(message, "")), nil
}

func readSSHKeyBundleUpload(context *gin.Context) ([]byte, error) {
	fileHeader, err := context.FormFile(sshKeyBundleUploadField)
	if err != nil {
		fileHeader, err = context.FormFile(sshKeyBundleUploadAlias)
	}
	if err != nil {
		fileHeader, err = context.FormFile(sshKeyBundleUploadAliasAlt)
	}
	if err != nil {
		return nil, fmt.Errorf("ssh key file required")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimitedSSHKeyBundle(file)
}

func readLimitedSSHKeyBundle(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, sshKeyBundleMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ssh key file is empty")
	}
	if len(data) > sshKeyBundleMaxBytes {
		return nil, fmt.Errorf("ssh key file is too large")
	}
	return data, nil
}

func readSSHKeyFilesFromZip(payload []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("invalid ssh key file")
	}
	files := make(map[string][]byte)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := normalizeSSHKeyBundleEntryName(file.Name)
		if name == "" {
			continue
		}
		if file.UncompressedSize64 > sshKeyFileMaxBytes {
			return nil, fmt.Errorf("%s is too large", name)
		}
		openFile, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := readLimitedSSHKeyFile(openFile, name)
		closeErr := openFile.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files[name] = content
	}
	if len(files[sshKeyPrivateName]) == 0 {
		return nil, fmt.Errorf("%s missing from ssh key file", sshKeyPrivateName)
	}
	if len(files[sshKeyPublicName]) == 0 {
		return nil, fmt.Errorf("%s missing from ssh key file", sshKeyPublicName)
	}
	files[sshKeyAuthorizedName] = files[sshKeyPublicName]
	return files, nil
}

func normalizeSSHKeyBundleEntryName(name string) string {
	name = filepath.Base(filepath.ToSlash(strings.TrimSpace(name)))
	switch name {
	case sshKeyPrivateName, sshKeyPublicName:
		return name
	default:
		return ""
	}
}

func readLimitedSSHKeyFile(reader io.Reader, name string) ([]byte, error) {
	limited := io.LimitReader(reader, sshKeyFileMaxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s file is empty", name)
	}
	if len(data) > sshKeyFileMaxBytes {
		return nil, fmt.Errorf("%s file is too large", name)
	}
	return data, nil
}

func ensureSSHKeyDirectory() error {
	dir := resolveSSHKeyDirectory()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func writeSSHKeyFiles(files map[string][]byte) error {
	paths := sshKeyPaths()
	for _, name := range sshKeyFileNames() {
		content, ok := files[name]
		if !ok {
			return fmt.Errorf("%s content required", name)
		}
		if len(content) == 0 {
			return fmt.Errorf("%s content is empty", name)
		}
		mode := sshKeyFileModes[name]
		path := paths[name]
		if err := os.WriteFile(path, content, mode); err != nil {
			return err
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
	}
	return nil
}

func existingSSHKeyFiles() []string {
	paths := sshKeyPaths()
	existing := make([]string, 0, len(paths))
	for _, name := range sshKeyFileNames() {
		if _, err := os.Stat(paths[name]); err == nil {
			existing = append(existing, name)
		}
	}
	return existing
}

func sshKeyResult(message string, filename string) SSHKeyResult {
	return SSHKeyResult{
		Message:   message,
		Directory: resolveSSHKeyDirectory(),
		Filename:  filename,
		Files:     sshKeyFileInfos(),
	}
}

func sshKeyFileInfos() []SSHKeyFileInfo {
	paths := sshKeyPaths()
	infos := make([]SSHKeyFileInfo, 0, len(paths))
	for _, name := range sshKeyFileNames() {
		path := paths[name]
		info := SSHKeyFileInfo{
			Name: name,
			Path: path,
		}
		stat, err := os.Stat(path)
		if err == nil {
			info.Exists = true
			info.Size = stat.Size()
			info.Mode = fmt.Sprintf("%04o", stat.Mode().Perm())
			info.ModifiedAt = stat.ModTime().Format(time.RFC3339)
		}
		infos = append(infos, info)
	}
	return infos
}

func sshKeyPaths() map[string]string {
	dir := resolveSSHKeyDirectory()
	return map[string]string{
		sshKeyPrivateName:    filepath.Join(dir, sshKeyPrivateName),
		sshKeyPublicName:     filepath.Join(dir, sshKeyPublicName),
		sshKeyAuthorizedName: filepath.Join(dir, sshKeyAuthorizedName),
	}
}

func sshKeyFileNames() []string {
	return []string{sshKeyPrivateName, sshKeyPublicName, sshKeyAuthorizedName}
}

func resolveSSHKeyDirectory() string {
	if dir := strings.TrimSpace(os.Getenv("ABLESTACK_SSH_KEY_DIR")); dir != "" {
		return dir
	}
	return sshKeyDefaultDir
}

func sshKeyResponse(action string, code int, message string, val any) SSHKeyResponse {
	return SSHKeyResponse{
		Code:    code,
		Val:     val,
		RetName: sshKeyDefaultRetName,
		Message: message,
		Action:  action,
	}
}

func sshKeyError(action string, code int, message string) SSHKeyResponse {
	return sshKeyResponse(action, code, message, message)
}

func statusCodeFromSSHKeyResponse(resp SSHKeyResponse) int {
	if resp.Code >= 100 && resp.Code <= 599 {
		return resp.Code
	}
	return http.StatusOK
}
