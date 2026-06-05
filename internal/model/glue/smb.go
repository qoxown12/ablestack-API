package glue

// SMBShareRequest는 SMB service와 최초 share를 생성하는 요청이다.
// @name GlueSMBShareRequest
type SMBShareRequest struct {
	SecurityType string `json:"sec_type" example:"normal" enums:"normal,ads"`
	CachePolicy  string `json:"cache_policy" example:"true" enums:"true,false"`
	Username     string `json:"username" example:"smbuser"`
	Password     string `json:"password" example:"password"`
	FolderName   string `json:"folder_name" example:"share01"`
	Path         string `json:"path" example:"/gluefs/volumes/share01"`
	FSName       string `json:"fs_name" example:"gluefs"`
	VolumePath   string `json:"volume_path" example:"/volumes/share01"`
	Realm        string `json:"realm,omitempty" example:"EXAMPLE.LOCAL"`
	DNS          string `json:"dns,omitempty" example:"10.10.10.10"`
}

// SMBFolderRequest는 SMB share folder를 추가하는 요청이다.
// @name GlueSMBFolderRequest
type SMBFolderRequest struct {
	CachePolicy string `json:"cache_policy" example:"true" enums:"true,false"`
	FolderName  string `json:"folder_name" example:"share02"`
	Path        string `json:"path" example:"/gluefs/volumes/share02"`
	FSName      string `json:"fs_name" example:"gluefs"`
	VolumePath  string `json:"volume_path" example:"/volumes/share02"`
}

// SMBFolderDeleteRequest는 SMB share folder를 삭제하는 요청이다.
// @name GlueSMBFolderDeleteRequest
type SMBFolderDeleteRequest struct {
	FolderName string `json:"folder_name" example:"share02"`
	Path       string `json:"path" example:"/gluefs/volumes/share02"`
	FSName     string `json:"fs_name" example:"gluefs"`
}

// SMBUserRequest는 SMB user 생성/수정 요청이다.
// @name GlueSMBUserRequest
type SMBUserRequest struct {
	Username string `json:"username" example:"smbuser"`
	Password string `json:"password" example:"password"`
}

// SMBUserDeleteRequest는 SMB user 삭제 요청이다.
// @name GlueSMBUserDeleteRequest
type SMBUserDeleteRequest struct {
	Username string `json:"username" example:"smbuser"`
}
