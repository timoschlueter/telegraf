//go:build !custom || inputs || inputs.librelinkup

package all

import _ "github.com/influxdata/telegraf/plugins/inputs/librelinkup" // register plugin
