package _189_share

import "github.com/OpenListTeam/OpenList/v4/internal/model"

type ShareInfo struct {
	FileId    string `json:"fileId"`
	ShareMode int    `json:"shareMode"`
	ShareId   int    `json:"shareId"`
	IsFolder  bool   `json:"isFolder"`
}

type Cloud189FilesResp struct {
	//ResCode    int    `json:"res_code"`
	//ResMessage string `json:"res_message"`
	FileListAO struct {
		Count      int              `json:"count"`
		FileList   []Cloud189File   `json:"fileList"`
		FolderList []Cloud189Folder `json:"folderList"`
	} `json:"fileListAO"`
}

type Cloud189Folder struct {
	ID       String `json:"id"`
	ParentID int64  `json:"parentId"`
	Name     string `json:"name"`

	LastOpTime Time `json:"lastOpTime"`
	CreateDate Time `json:"createDate"`

	// FileListSize int64 `json:"fileListSize"`
	// FileCount int64 `json:"fileCount"`
	// FileCata  int64 `json:"fileCata"`
	// Rev          string `json:"rev"`
	// StarLabel    int64  `json:"starLabel"`
}

/*文件部分*/
// 文件
type Cloud189File struct {
	ID   String `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Md5  string `json:"md5"`

	LastOpTime Time `json:"lastOpTime"`
	CreateDate Time `json:"createDate"`
	Icon       struct {
		//iconOption 5
		SmallUrl string `json:"smallUrl"`
		LargeUrl string `json:"largeUrl"`

		// iconOption 10
		Max600    string `json:"max600"`
		MediumURL string `json:"mediumUrl"`
	} `json:"icon"`

	// Orientation int64  `json:"orientation"`
	// FileCata   int64  `json:"fileCata"`
	// MediaType   int    `json:"mediaType"`
	// Rev         string `json:"rev"`
	// StarLabel   int64  `json:"starLabel"`
}

type FileObj struct {
	model.ObjThumb
	oldName string
}
