package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"spuri/internal/storage"
	"spuri/internal/utils"
)

func GetStorageQuota(c *gin.Context) {
	p := getStorageProvider(c)
	if p == nil {
		var err error
		p, err = storage.NewStorageProvider()
		if err != nil {
			utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
			return
		}
	}
	q, err := p.GetQuota()
	if err != nil {
		utils.RespondWithError(c, http.StatusServiceUnavailable, err.Error(), err)
		return
	}
	academias := make([]gin.H, 0, len(q.Academias))
	for _, a := range q.Academias {
		academias = append(academias, gin.H{"codigo_academia": a.CodigoAcademia, "used_bytes": a.UsedBytes, "used_human": storage.HumanBytes(a.UsedBytes)})
	}
	files := make([]gin.H, 0, len(q.AccountFiles))
	for _, f := range q.AccountFiles {
		files = append(files, gin.H{"path": f.Path, "name": f.Name, "size_bytes": f.SizeBytes, "size_human": storage.HumanBytes(f.SizeBytes), "managed": f.Managed})
	}
	folders := make([]gin.H, 0, len(q.AccountFolders))
	for _, f := range q.AccountFolders {
		folders = append(folders, gin.H{"path": f.Path, "name": f.Name, "size_bytes": f.SizeBytes, "size_human": storage.HumanBytes(f.SizeBytes), "managed": f.Managed})
	}
	c.JSON(http.StatusOK, gin.H{"provider": p.ProviderName(), "total_bytes": q.TotalBytes, "used_bytes": q.UsedBytes, "available_bytes": q.AvailableBytes, "managed_bytes": q.ManagedBytes, "outside_academias_bytes": q.OutsideAcademiasBytes, "unmanaged_bytes": q.UnmanagedBytes, "total_human": storage.HumanBytes(q.TotalBytes), "used_human": storage.HumanBytes(q.UsedBytes), "available_human": storage.HumanBytes(q.AvailableBytes), "managed_human": storage.HumanBytes(q.ManagedBytes), "outside_academias_human": storage.HumanBytes(q.OutsideAcademiasBytes), "unmanaged_human": storage.HumanBytes(q.UnmanagedBytes), "academias": academias, "account_files": files, "account_folders": folders})
}
