package controller

import (
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ablecloud.io/ablestack-api/internal/infra/logging"
	"ablecloud.io/ablestack-api/internal/infra/utils"
	Cube "ablecloud.io/ablestack-api/internal/model/cube"
)

const controllerHandlerInterval = 30 * time.Second

type TypeController struct {
	Handlers []func()           `json:"handlers"`
	running  bool               `json:"running"`
	errors   *utils.Errors      `json:"errors"`
	version  *utils.TypeVersion `json:"version"`
	Cube     *Cube.TypeCUBE     `json:"cube"`
} //	@name	TypeController

var lockController sync.Once
var controller *TypeController

func Init() *TypeController {
	if controller == nil {
		lockController.Do(
			func() {
				fmt.Println("Creating ", reflect.TypeOf(controller), " now.")
				controller = &TypeController{}
				controller.Cube = Cube.Cube()
				controller.errors = &utils.Errors{}
			})
	} else {
		fmt.Println("get old ", reflect.TypeOf(controller), " instance.")
	}
	return controller
}

func (c *TypeController) StatusRegister(fn func()) {

	c.Handlers = append(c.Handlers, fn)
}

func (c *TypeController) Start() {
	c.running = true
	for c.running {
		for _, handler := range c.Handlers {
			go runRegisteredHandler(handler)
		}

		time.Sleep(controllerHandlerInterval)
	}
}

func runRegisteredHandler(handler func()) {
	job := registeredHandlerName(handler)
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.RecordJobPanic(job, recovered, nil)
		}
	}()
	handler()
}

func registeredHandlerName(handler func()) string {
	if handler == nil {
		return "controller.unknown"
	}
	name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

func (c *TypeController) Stop() {
	c.running = false
}

func (c *TypeController) AddError(err error) {
	serr := err.Error()
	c.errors.Errors = append(c.errors.Errors, utils.Errorlog{Error: serr, Time: time.Now()})
}

func AddError(err error) {
	Init()
	controller.AddError(err)
}

func (c *TypeController) GetError() *utils.Errors {
	return c.errors
}

func (c *TypeController) ClearError() {
	c.errors = &utils.Errors{}
}

// Error godoc
//
//	@Summary		Error
//	@Description	Error.
//	@Tags			Cube-Error
//	@Accept			x-www-form-urlencoded
//	@Produce		json
//	@Success		200	{object}	utils.Errorlog
//	@Failure		400	{object}	HTTP400BadRequest
//	@Failure		404	{object}	HTTP404NotFound
//	@Failure		500	{object}	HTTP500InternalServerError
//	@Router			/err [get]
func (c *TypeController) Error(ctx *gin.Context) {
	ctx.IndentedJSON(http.StatusOK, c.GetError())
}

func (c *TypeController) DeleteError(context *gin.Context) {
	c.ClearError()
	context.IndentedJSON(http.StatusOK, c.GetError())
}
