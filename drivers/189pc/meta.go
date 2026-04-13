package _189pc

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	LoginType    string `json:"login_type" type:"select" options:"password,qrcode" default:"password" required:"true"`
	Username     string `json:"username" required:"true"`
	Password     string `json:"password" required:"true"`
	VCode        string `json:"validate_code"`
	RefreshToken string `json:"refresh_token" help:"To switch accounts, please clear this field"`
	driver.RootID
	OrderBy                    string `json:"order_by" type:"select" options:"filename,filesize,lastOpTime" default:"filename"`
	OrderDirection             string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
	Type                       string `json:"type" type:"select" options:"personal,family" default:"personal"`
	FamilyID                   string `json:"family_id"`
	UploadMethod               string `json:"upload_method" type:"select" options:"stream,rapid,old" default:"stream"`
	UploadThread               string `json:"upload_thread" default:"3" help:"1<=thread<=32"`
	FamilyTransfer             bool   `json:"family_transfer"`
	RapidUpload                bool   `json:"rapid_upload"`
	NoUseOcr                   bool   `json:"no_use_ocr"`
	GenerateCAS                bool   `json:"generate_cas" help:"上传文件后，在同目录生成一个同名的 .cas 元数据文件"`
	DeleteSource               bool   `json:"delete_source" help:"成功生成 .cas 文件后，自动删除原始源文件"`
	RestoreSourceFromCAS       bool   `json:"restore_source_from_cas" help:"上传 .cas 文件时，尝试根据其中的哈希信息秒传还原源文件，而不是直接上传 .cas 文件本身"`
	RestoreSourceUseCurrentName bool  `json:"restore_source_use_current_name" help:"从 .cas 还原源文件时，使用当前 .cas 文件名去掉 .cas 后缀后的名称；如果没有扩展名，会尽量补上原始扩展名"`
	DeleteCASAfterRestore      bool   `json:"delete_cas_after_restore" help:"从已有 .cas 成功还原出源文件后，自动删除该 .cas 文件；如果源文件已存在，也会清理该 .cas 文件"`
	AutoRestoreExistingCAS     bool   `json:"auto_restore_existing_cas" help:"自动监视已配置目录中的 .cas 文件，检测到变化时立即尝试在后台还原源文件"`
	AutoRestoreExistingCASPaths string `json:"auto_restore_existing_cas_paths" type:"text" help:"要监视的目录路径，每行一个，路径相对于当前存储根目录；会自动包含其下所有子目录"`
	GenerateCASAndDeleteSource bool   `json:"generate_cas_and_delete_source" ignore:"true"`

	AutoCheckin bool   `json:"auto_checkin"`
	Cookie      string `json:"cookie"`

	Concurrency int `json:"concurrency" type:"number" default:"1"`
	ChunkSize   int `json:"chunk_size" type:"number" default:"1024"`
}

var config = driver.Config{
	Name:        "189CloudPC",
	DefaultRoot: "-11",
	CheckStatus: true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Cloud189PC{}
	})
}
