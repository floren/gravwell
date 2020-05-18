module github.com/gravwell/gravwell/v3

go 1.13

require (
	cloud.google.com/go/pubsub v1.3.1
	collectd.org v0.3.1-0.20181025072142-f80706d1e115
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/Shopify/sarama v1.24.1
	github.com/aws/aws-sdk-go v1.25.46
	github.com/buger/jsonparser v0.0.0-20191004114745-ee4c978eae7e
	github.com/bxcodec/faker/v3 v3.3.1
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/elastic/beats v7.6.2+incompatible
	github.com/floren/ipfix v1.4.1
	github.com/floren/o365 v0.0.1
	github.com/fsnotify/fsnotify v1.4.9
	github.com/google/go-write v0.0.0-20181107114627-56629a6b2542
	github.com/google/gopacket v1.1.17
	github.com/google/uuid v1.1.1
	github.com/gravwell/gcfg v1.2.5
	github.com/gravwell/ingest/v3 v3.3.12
	github.com/gravwell/ingesters/v3 v3.3.12
	github.com/h2non/filetype v1.0.10
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901
	github.com/klauspost/compress v1.9.3
	github.com/minio/highwayhash v1.0.0
	github.com/shirou/gopsutil v2.19.11+incompatible
	github.com/stretchr/testify v1.5.1
	github.com/tealeg/xlsx v1.0.5
	github.com/turnage/graw v0.0.0-20191104042329-405cc3092119
	go.etcd.io/bbolt v1.3.4
	golang.org/x/sys v0.0.0-20200302150141-5c8b2ff67527
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1
)

// Leave this until https://github.com/buger/jsonparser/pull/180 is merged
replace github.com/buger/jsonparser => github.com/floren/jsonparser v0.0.0-20191025224154-2951042f1c13
