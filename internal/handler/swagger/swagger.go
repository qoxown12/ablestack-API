package swagger

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"ablecloud.io/ablestack-api/docs"
	GlueHandler "ablecloud.io/ablestack-api/internal/handler/glue"
)

// Handler는 Swagger UI asset은 gin-swagger에 맡기고 doc.json만 role에 맞게 필터링한다.
// host/CCVM에서는 실제 route가 등록되지 않는 Glue path를 Swagger 화면에서도 숨긴다.
func Handler() gin.HandlerFunc {
	ui := ginSwagger.WrapHandler(swaggerFiles.Handler)
	return func(ctx *gin.Context) {
		if strings.HasSuffix(ctx.Request.URL.Path, "/doc.json") {
			doc, err := FilterDocForSCVM(docs.SwaggerInfo.ReadDoc(), GlueHandler.IsSCVMNode())
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			ctx.Data(http.StatusOK, "application/json; charset=utf-8", doc)
			return
		}
		ui(ctx)
	}
}

func FilterDocForSCVM(raw string, scvm bool) ([]byte, error) {
	if scvm {
		return []byte(raw), nil
	}

	var doc map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}

	removeGluePaths(doc)
	removeGlueTags(doc)
	removeGlueDefinitions(doc)

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func removeGluePaths(doc map[string]any) {
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return
	}
	for path := range paths {
		if path == "/glue" || strings.HasPrefix(path, "/glue/") {
			delete(paths, path)
		}
	}
}

func removeGlueTags(doc map[string]any) {
	rawTags, ok := doc["tags"].([]any)
	if !ok {
		return
	}
	tags := make([]any, 0, len(rawTags))
	for _, rawTag := range rawTags {
		tag, ok := rawTag.(map[string]any)
		if !ok {
			tags = append(tags, rawTag)
			continue
		}
		name, _ := tag["name"].(string)
		if strings.HasPrefix(name, "Glue") {
			continue
		}
		tags = append(tags, rawTag)
	}
	doc["tags"] = tags
}

func removeGlueDefinitions(doc map[string]any) {
	definitions, ok := doc["definitions"].(map[string]any)
	if !ok {
		return
	}
	for name := range definitions {
		if strings.Contains(name, "_internal_model_glue.") || strings.Contains(name, "/internal/model/glue.") {
			delete(definitions, name)
		}
	}
}
