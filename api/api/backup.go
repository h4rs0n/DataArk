package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// backup.go 提供备份创建与恢复 HTTP 接口。

// CreateBackup 创建并下载备份 zip。
func CreateBackup(c *gin.Context) {
	preparedBackup, err := createBackupArchive(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "创建备份失败",
			"Error":   err.Error(),
		})
		return
	}
	defer preparedBackup.Cleanup()

	reader, writer := io.Pipe()
	go func() {
		err := preparedBackup.WriteZip(writer)
		_ = writer.CloseWithError(err)
	}()

	c.DataFromReader(http.StatusOK, -1, "application/zip", reader, map[string]string{
		"Content-Disposition": fmt.Sprintf("attachment; filename=%q", preparedBackup.FileName),
		"Cache-Control":       "no-store",
	})
}

// RestoreBackup 从上传的 zip 恢复备份。
func RestoreBackup(c *gin.Context) {
	backupFile, err := c.FormFile("file")
	if err != nil {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "缺少备份文件",
		})
		return
	}
	if !strings.EqualFold(filepath.Ext(backupFile.Filename), ".zip") {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "备份文件必须是 zip 压缩包",
		})
		return
	}

	tempDir, err := os.MkdirTemp("", "dataark-restore-upload-*")
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "初始化恢复临时目录失败",
			"Error":   err.Error(),
		})
		return
	}
	defer os.RemoveAll(tempDir)

	zipPath := filepath.Join(tempDir, "backup.zip")
	if err := c.SaveUploadedFile(backupFile, zipPath); err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "保存备份文件失败",
			"Error":   err.Error(),
		})
		return
	}

	result, err := restoreBackupArchive(c.Request.Context(), zipPath)
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "恢复备份失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "备份恢复成功",
		"Data":    result,
	})
}
