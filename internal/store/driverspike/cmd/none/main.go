// Command none is the size baseline: database/sql without a driver.
package main

import (
	"database/sql"
	"fmt"
)

func main() { fmt.Println(sql.Drivers()) }
