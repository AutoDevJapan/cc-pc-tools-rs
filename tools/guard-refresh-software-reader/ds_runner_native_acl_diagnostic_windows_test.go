//go:build windows

package main

import (
    "fmt"
    "strings"
    "testing"
)

// Report no SID, ACE, path or descriptor text; equality is diagnostic only.
func dsRunnerSDDLParts(s string)(owner,group,flags,aces string,ok bool){
    if len(s)>1<<20||!strings.HasPrefix(s,"O:"){return}
    g:=strings.Index(s,"G:");d:=strings.Index(s,"D:");if g<2||d<g+2||strings.Contains(s,"S:"){return}
    owner=s[2:g];group=s[g+2:d];rest:=s[d+2:]
    open:=strings.IndexByte(rest,'(');if open<0{flags=rest}else{flags=rest[:open];aces=rest[open:]}
    if owner==""||group==""{return};return owner,group,flags,aces,true
}
func dsRunnerACLDiagnostic(old,post string)string{
    oo,og,of,oa,ok1:=dsRunnerSDDLParts(old);po,pg,pf,pa,ok2:=dsRunnerSDDLParts(post)
    if !ok1||!ok2{return "sddl_components=unavailable"}
    return fmt.Sprintf("sddl_components=available owner_equal=%t group_equal=%t dacl_flags_equal=%t dacl_aces_equal=%t old_protected=%t post_protected=%t old_auto_inherited=%t post_auto_inherited=%t old_auto_requested=%t post_auto_requested=%t",oo==po,og==pg,of==pf,oa==pa,strings.Contains(of,"P"),strings.Contains(pf,"P"),strings.Contains(of,"AI"),strings.Contains(pf,"AI"),strings.Contains(of,"AR"),strings.Contains(pf,"AR"))
}
func TestDSRunnerACLDiagnosticDoesNotExposeDescriptor(t *testing.T){
    a:="O:S-1-0-0G:S-1-0-0D:PAI(A;;FA;;;SY)";b:="O:S-1-0-0G:S-1-0-0D:AI(A;;FA;;;SY)(D;;FW;;;WD)"
    got:=dsRunnerACLDiagnostic(a,b)
    if strings.Contains(got,"S-1-")||strings.Contains(got,";;;SY")||!strings.Contains(got,"owner_equal=true")||!strings.Contains(got,"dacl_aces_equal=false") {t.Fatal("ACL diagnostic leaked text or missed difference")}
    if dsRunnerACLDiagnostic("not-sddl",b)!="sddl_components=unavailable"{t.Fatal("malformed diagnostic accepted")}
    first:="O:SYG:SYD:(A;;FR;;;SY)(D;;FW;;;WD)";reordered:="O:SYG:SYD:(D;;FW;;;WD)(A;;FR;;;SY)"
    if !strings.Contains(dsRunnerACLDiagnostic(first,reordered),"dacl_aces_equal=false"){t.Fatal("ACE order change was hidden")}
}
