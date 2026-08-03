package licenseservice

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	authSecretContext = "ablestack-api-auth-v1"
	licenseDateLayout = "2006-01-02"
)

var (
	ErrNoLicense   = errors.New("registered license not found")
	ErrInvalid     = errors.New("invalid license")
	ErrExpired     = errors.New("license expired")
	ErrNotYetValid = errors.New("license not yet valid")
	ErrInactive    = errors.New("license inactive")
	ErrLicenseKey  = errors.New("license_key required")
)

type Info struct {
	LicenseKey     string `json:"license_key"`
	Issued         string `json:"issued"`
	Expired        string `json:"expired"`
	Status         string `json:"status"`
	OEM            string `json:"oem"`
	ProductName    string `json:"product_name"`
	ProductVersion string `json:"product_version"`
}

type Status struct {
	Active   bool
	Status   string
	Expired  string
	Issued   string
	OEM      string
	FilePath string
	Message  string
}

func CurrentStatus() (Status, error) {
	info, path, err := CurrentInfo()
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Active:   true,
		Status:   "active",
		Expired:  info.Expired,
		Issued:   info.Issued,
		OEM:      info.OEM,
		FilePath: path,
	}
	if err := ValidateActive(info); err != nil {
		status.Active = false
		status.Status = "inactive"
		status.Message = err.Error()
	}
	return status, nil
}

func CurrentAuthSecret() (string, error) {
	info, _, err := CurrentInfo()
	if err != nil {
		return "", err
	}
	if err := ValidateActive(info); err != nil {
		return "", err
	}
	return DeriveAuthSecret(info.LicenseKey)
}

func HasActiveLicense() bool {
	_, err := CurrentAuthSecret()
	return err == nil
}

func CurrentInfo() (Info, string, error) {
	licenseFile, err := CurrentLicenseFile()
	if err != nil {
		return Info{}, "", err
	}
	raw, err := os.ReadFile(licenseFile)
	if err != nil {
		return Info{}, "", err
	}
	info, err := DecryptInfo(strings.TrimSpace(string(raw)))
	if err != nil {
		return Info{}, "", err
	}
	return info, licenseFile, nil
}

func CurrentLicenseFile() (string, error) {
	hostUUID, err := ReadMachineID()
	if err != nil {
		return "", err
	}
	licenseDir := filepath.Join("/usr/share", hostUUID)
	files, err := os.ReadDir(licenseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoLicense
		}
		return "", err
	}
	fileNames := make([]string, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		fileNames = append(fileNames, entry.Name())
	}
	if len(fileNames) == 0 {
		return "", ErrNoLicense
	}
	sort.Strings(fileNames)
	return filepath.Join(licenseDir, fileNames[len(fileNames)-1]), nil
}

func Register(encodedContent string, filename string) (Status, error) {
	licenseContent, err := DecodeContent(encodedContent)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	info, err := DecryptInfo(licenseContent)
	if err != nil {
		return Status{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if err := ValidateActive(info); err != nil {
		return Status{}, err
	}

	hostUUID, err := ReadMachineID()
	if err != nil {
		return Status{}, err
	}
	licenseDir := filepath.Join("/usr/share", hostUUID)
	if err := os.MkdirAll(licenseDir, 0o700); err != nil {
		return Status{}, err
	}

	filename = strings.TrimSpace(filename)
	if filename != "" {
		filename = filepath.Base(filename)
	}
	if filename == "" || filename == "." || filename == ".." {
		return Status{}, fmt.Errorf("license filename required")
	}

	tempPath := filepath.Join(licenseDir, fmt.Sprintf(".%s.tmp.%d", filename, time.Now().UnixNano()))
	if err := os.WriteFile(tempPath, []byte(licenseContent), fs.FileMode(0o600)); err != nil {
		return Status{}, err
	}
	defer os.Remove(tempPath)
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return Status{}, err
	}

	newPath := filepath.Join(licenseDir, filename)
	if err := os.Rename(tempPath, newPath); err != nil {
		return Status{}, err
	}
	entries, _ := os.ReadDir(licenseDir)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == filename {
			continue
		}
		_ = os.Remove(filepath.Join(licenseDir, entry.Name()))
	}

	return Status{
		Active:   true,
		Status:   "active",
		Expired:  info.Expired,
		Issued:   info.Issued,
		OEM:      info.OEM,
		FilePath: newPath,
	}, nil
}

func ValidateActive(info Info) error {
	if strings.TrimSpace(info.LicenseKey) == "" {
		return ErrLicenseKey
	}
	if info.Issued == "" || info.Expired == "" {
		return fmt.Errorf("%w: issued or expired date missing", ErrInvalid)
	}
	if status := strings.TrimSpace(info.Status); status != "" && !strings.EqualFold(status, "active") {
		return ErrInactive
	}

	now := time.Now()
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	issued, err := time.ParseInLocation(licenseDateLayout, info.Issued, location)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	expired, err := time.ParseInLocation(licenseDateLayout, info.Expired, location)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if today.Before(issued) {
		return ErrNotYetValid
	}
	if today.After(expired) {
		return ErrExpired
	}
	return nil
}

func DeriveAuthSecret(licenseKey string) (string, error) {
	licenseKey = strings.TrimSpace(licenseKey)
	if licenseKey == "" {
		return "", ErrLicenseKey
	}
	mac := hmac.New(sha256.New, []byte(licenseKey))
	mac.Write([]byte(authSecretContext))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func ReadMachineID() (string, error) {
	raw, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func DecodeContent(encoded string) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", fmt.Errorf("license content required")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func DecryptInfo(base64Content string) (Info, error) {
	const (
		password = "password"
		salt     = "salt"
	)

	key, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 32)
	if err != nil {
		return Info{}, err
	}
	iv, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 16)
	if err != nil {
		return Info{}, err
	}

	encryptedBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64Content))
	if err != nil {
		return Info{}, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return Info{}, err
	}
	if len(encryptedBytes)%aes.BlockSize != 0 {
		return Info{}, fmt.Errorf("invalid encrypted size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(encryptedBytes))
	mode.CryptBlocks(plain, encryptedBytes)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return Info{}, err
	}

	var info Info
	if err := json.Unmarshal(plain, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

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
