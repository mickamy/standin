package infer

import "go/types"

var (
	NameExpr = nameExpr
	TypeExpr = typeExpr
)

func TagExpr(tag string, typ types.Type, pkgPath, pkgName string) string {
	inf := inferrer{pkgPath: pkgPath, pkgName: pkgName}

	return inf.tagExpr(tag, typ)
}
