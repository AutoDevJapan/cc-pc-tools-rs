package main

import("path/filepath";"testing")

func TestPortableWindowsNamespaces(t *testing.T){for _,c:=range []struct{in,out string;valid bool}{
 {`D:\CC\Work`, `D:\CC\Work`,true},{`\\?\D:\CC\Work`,`D:\CC\Work`,true},
 {`\\?\UNC\server\share\file`,`\\server\share\file`,true},
 {`\\?\Volume{123}\file`,"",false},{`\\.\PhysicalDrive0`,"",false},{`\\?\GLOBALROOT\Device\x`,"",false},
 }{out,e:=portable(c.in);if (e==nil)!=c.valid||out!=c.out{t.Fatalf("namespace normalization differs")}}}
func TestContainmentRejectsSiblingAndParent(t *testing.T){root:=filepath.Join(t.TempDir(),"root");if !within(root,filepath.Join(root,"child")){t.Fatal("child rejected")};for _,p:=range []string{root+"-sibling",filepath.Dir(root),`C:\other`}{if within(root,p){t.Fatal("outside path accepted")}}}
func TestUnknownResourceRemainsProtected(t *testing.T){if allowed(filepath.Join(t.TempDir(),"unknown")){t.Fatal("unknown resource accepted")}}
