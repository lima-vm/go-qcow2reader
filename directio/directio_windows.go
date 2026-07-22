package directio

// Flag is the open flag for direct I/O. Windows supports direct I/O via
// FILE_FLAG_NO_BUFFERING (0x20000000), but syscall.Open only passes this flag
// to CreateFile since Go 1.26. When we require Go 1.26, set Flag to
// 0x20000000 and DefaultAlignment to 4096.
// https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilea#FILE_FLAG_NO_BUFFERING
const Flag = 0

// DefaultAlignment is 0 since Flag is not set.
const DefaultAlignment = 0
