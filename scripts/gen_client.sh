#!/bin/sh

location=$(dirname $0)

#cd $location/../pkg/client/kuadrant

echo "Generating Go client code..."

rm -rf pkg/client/kuadrant/clientset

mkdir -p github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset

go run k8s.io/code-generator/cmd/client-gen \
	--input=kuadrant/v1 \
	--go-header-file=scripts/boilerplate.txt \
	--clientset-name "versioned" \
	--input-base=github.com/kuadrant/kcp-glbc/pkg/apis \
	--output-base=. \
	--output-package=github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset

go run k8s.io/code-generator/cmd/lister-gen \
	--input-dirs=github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1 \
	--go-header-file=scripts/boilerplate.txt \
	--output-base=. \
	--output-package=github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/listers

go run k8s.io/code-generator/cmd/informer-gen \
    --versioned-clientset-package=github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/clientset/versioned \
	--listers-package=github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/listers \
	--input-dirs=github.com/kuadrant/kcp-glbc/pkg/apis/kuadrant/v1 \
	--go-header-file=scripts/boilerplate.txt \
	--output-base=. \
	--output-package=github.com/kuadrant/kcp-glbc/pkg/client/kuadrant/informers

cp -R ./github.com/kuadrant/kcp-glbc/pkg/client/kuadrant pkg/client
rm -rf ./github.com
