include $(GOROOT)/src/Make.inc

TARG=http/uwsgi
GOFILES=\
	uwsgi.go\

include $(GOROOT)/src/Make.pkg
