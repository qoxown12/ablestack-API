package cube

import (
	"ablecloud.io/ablestack-api/internal/infra/utils"
	"fmt"
	"reflect"
	"sync"
	"time"
)

type TypeCUBE struct {
	Disks       *TypeBlockDevice `json:"disk"`
	NICs        *TypeNICStatus   `json:"nic"`
	Hosts       *TypeHosts       `json:"hosts"`
	RefreshTime time.Time        `json:"refreshTime"`
}

var lockCUBE sync.Once
var cube *TypeCUBE

func Cube() *TypeCUBE {
	if cube == nil {
		lockCUBE.Do(
			func() {
				fmt.Println("Creating ", reflect.TypeOf(cube), " now.")
				cube = &TypeCUBE{
					Disks: Disk(),
					NICs:  NIC(),
					Hosts: Hosts(),
				}
			})
	} else {
		fmt.Println("get old ", reflect.TypeOf(cube), " instance.")
	}

	return cube
}

func (c *TypeCUBE) GetVersion() utils.TypeVersion {
	return utils.TypeVersion{}
} // @name version

func (c *TypeCUBE) Update() utils.TypeVersion {
	return utils.TypeVersion{}
} // @name version
