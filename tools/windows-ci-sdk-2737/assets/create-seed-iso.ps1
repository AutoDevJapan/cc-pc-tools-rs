param([Parameter(Mandatory=$true)][string]$SourceDir,[Parameter(Mandatory=$true)][string]$Destination)
$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $SourceDir -PathType Container)) { throw 'seed directory missing' }
Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Runtime.InteropServices;
public static class SDK2737ImageStreamWriter {
  [UnmanagedFunctionPointer(CallingConvention.StdCall)]
  private delegate int ReadDelegate(IntPtr self, [Out, MarshalAs(UnmanagedType.LPArray, SizeParamIndex=2)] byte[] buffer, uint count, IntPtr read);
  public static void CopyToFile(object streamObject, string destination) {
    IntPtr unknown = Marshal.GetIUnknownForObject(streamObject);
    Guid iid = new Guid("0000000c-0000-0000-C000-000000000046");
    IntPtr stream = IntPtr.Zero;
    IntPtr readCount = Marshal.AllocHGlobal(4);
    try {
      Marshal.ThrowExceptionForHR(Marshal.QueryInterface(unknown, in iid, out stream));
      IntPtr vtable = Marshal.ReadIntPtr(stream);
      IntPtr readAddress = Marshal.ReadIntPtr(vtable, 3 * IntPtr.Size);
      var read = (ReadDelegate)Marshal.GetDelegateForFunctionPointer(readAddress, typeof(ReadDelegate));
      using (var output = new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None)) {
        byte[] buffer = new byte[1024 * 1024];
        while (true) {
          Marshal.WriteInt32(readCount, 0);
          int hr = read(stream, buffer, (uint)buffer.Length, readCount);
          if (hr < 0) Marshal.ThrowExceptionForHR(hr);
          int n = Marshal.ReadInt32(readCount);
          if (n < 0 || n > buffer.Length) throw new IOException("invalid image stream byte count");
          if (n == 0) break;
          output.Write(buffer, 0, n);
        }
        output.Flush(true);
      }
    } finally {
      Marshal.FreeHGlobal(readCount);
      if (stream != IntPtr.Zero) Marshal.Release(stream);
      Marshal.Release(unknown);
    }
  }
}
'@
$image = New-Object -ComObject IMAPI2FS.MsftFileSystemImage
$image.ChooseImageDefaultsForMediaType(9)
$image.FileSystemsToCreate = 3
$image.VolumeName = 'CCSDK2737'
$image.Root.AddTree((Resolve-Path -LiteralPath $SourceDir).Path, $false)
$result = $image.CreateResultImage()
[SDK2737ImageStreamWriter]::CopyToFile($result.ImageStream, $Destination)
