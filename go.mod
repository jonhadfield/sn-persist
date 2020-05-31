module github.com/jonhadfield/sn-persist

go 1.14

require (
	github.com/asdine/storm/v3 v3.2.0
	github.com/jonhadfield/gosn-v2 v0.0.0-20200517210619-52110795737e
	github.com/stretchr/testify v1.5.1
	go.etcd.io/bbolt v1.3.4
)

replace github.com/jonhadfield/gosn-v2 => ../gosn-v2
