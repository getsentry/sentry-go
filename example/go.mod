module github.com/getsentry/sentry-go/echo/example

go 1.17

require (
	github.com/getsentry/sentry-go v0.12.0
	github.com/getsentry/sentry-go/echo v0.0.0
	github.com/getsentry/sentry-go/fasthttp v0.0.0
	github.com/getsentry/sentry-go/gin v0.0.0
	github.com/getsentry/sentry-go/iris v0.0.0
	github.com/getsentry/sentry-go/martini v0.0.0
	github.com/getsentry/sentry-go/negroni v0.0.0
	github.com/gin-gonic/gin v1.7.7
	github.com/go-martini/martini v0.0.0-20170121215854-22fa46961aab
	github.com/kataras/iris/v12 v12.1.8
	github.com/labstack/echo/v4 v4.5.0
	github.com/urfave/negroni v1.0.0
	github.com/valyala/fasthttp v1.6.0
)

require (
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/CloudyKit/fastprinter v0.0.0-20200109182630-33d98a066a53 // indirect
	github.com/CloudyKit/jet/v3 v3.0.0 // indirect
	github.com/Shopify/goreferrer v0.0.0-20181106222321-ec9c9a553398 // indirect
	github.com/aymerick/raymond v2.0.3-0.20180322193309-b565731e1464+incompatible // indirect
	github.com/codegangsta/inject v0.0.0-20150114235600-33e0aa1cb7c0 // indirect
	github.com/eknkc/amber v0.0.0-20171010120322-cdade1c07385 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.13.0 // indirect
	github.com/go-playground/universal-translator v0.17.0 // indirect
	github.com/go-playground/validator/v10 v10.4.1 // indirect
	github.com/golang-jwt/jwt v3.2.2+incompatible // indirect
	github.com/golang/protobuf v1.3.3 // indirect
	github.com/gopherjs/gopherjs v0.0.0-20181017120253-0766667cb4d1 // indirect
	github.com/iris-contrib/blackfriday v2.0.0+incompatible // indirect
	github.com/iris-contrib/jade v1.1.3 // indirect
	github.com/iris-contrib/pongo2 v0.0.1 // indirect
	github.com/iris-contrib/schema v0.0.1 // indirect
	github.com/json-iterator/go v1.1.9 // indirect
	github.com/jtolds/gls v4.20.0+incompatible // indirect
	github.com/kataras/golog v0.0.10 // indirect
	github.com/kataras/pio v0.0.2 // indirect
	github.com/kataras/sitemap v0.0.5 // indirect
	github.com/klauspost/compress v1.9.7 // indirect
	github.com/labstack/gommon v0.3.0 // indirect
	github.com/leodido/go-urn v1.2.0 // indirect
	github.com/mattn/go-colorable v0.1.11 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/microcosm-cc/bluemonday v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/ryanuber/columnize v2.1.0+incompatible // indirect
	github.com/schollz/closestmatch v2.1.0+incompatible // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/smartystreets/assertions v0.0.0-20180927180507-b2de0cb4f26d // indirect
	github.com/ugorji/go/codec v1.1.7 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.1 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519 // indirect
	golang.org/x/net v0.0.0-20211008194852-3b03d305991f // indirect
	golang.org/x/sys v0.0.0-20211007075335-d3039528d8ac // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20201208040808-7e3f01d25324 // indirect
	gopkg.in/ini.v1 v1.51.1 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
	gopkg.in/yaml.v3 v3.0.0-20191120175047-4206685974f2 // indirect
)

replace github.com/getsentry/sentry-go => ../

replace github.com/getsentry/sentry-go/echo => ../echo

replace github.com/getsentry/sentry-go/gin => ../gin

replace github.com/getsentry/sentry-go/iris => ../iris

replace github.com/getsentry/sentry-go/negroni => ../negroni

replace github.com/getsentry/sentry-go/fasthttp => ../fasthttp

replace github.com/getsentry/sentry-go/martini => ../martini
