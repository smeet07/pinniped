// This go.mod file is generated by ./hack/codegen.sh.
module go.pinniped.dev/generated/1.23/client

go 1.13

require (
	go.pinniped.dev/generated/1.23/apis v0.0.0
	k8s.io/apimachinery v0.23.17
	k8s.io/client-go v0.23.17
	k8s.io/kube-openapi v0.0.0-20211115234752-e816edb12b65
)

replace go.pinniped.dev/generated/1.23/apis => ../apis
