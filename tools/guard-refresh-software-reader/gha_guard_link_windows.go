//go:build windows

package main

import (
    "net/http"
    "strings"
)

// GitHub's API-version notice is informational and is not pagination. All
// other Link forms, including mixed or repeated fields, remain fail-closed.
const ghaGuardVersionDeprecationLink = `<https://docs.github.com/en/rest/about-the-rest-api/api-versions>; rel="deprecation"; type="text/html"`

func ghaGuardSinglePageLink(headers http.Header) bool {
    found:=false
    for name,values:=range headers {
        if !strings.EqualFold(name,"Link") {continue}
        if found||len(values)!=1 {return false}
        found=true
        if strings.Trim(values[0]," \t")!=ghaGuardVersionDeprecationLink {return false}
    }
    return true
}
