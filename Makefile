include $(GOROOT)/src/Make.inc

TARG=github.com/mattn/go-uwsgi
GOFILES=\
	uwsgi.go\

include $(GOROOT)/src/Make.pkg
