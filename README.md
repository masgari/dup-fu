# dup-fu
Terminal application for finding duplicate files.

![Screenshot](screenshot.png)

```sh
go get -v github.com/masgari/dup-fu
```

*Usage*

```sh
dup-fu [scan-dir] [target-dir]
```
`scan-dir` default = current directory 
`target-dir` default = `.dup-fu`

```
dup-fu . /tmp/duplicates
dup-fu /Volumens/MyBackup/ /tmp/duplicates

dup-fu c:\ d:\duplicates
```

*WARNING*

`Delete` remove duplicate files without confirm, be careful.