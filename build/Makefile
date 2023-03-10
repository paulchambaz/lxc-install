STRIP = strip
BIN_DIR = /usr/bin
MAN_DIR = /usr/share/man/man1

lxc-install: clean
	@go build -o lxc-install ../source

install: lxc-install
	@echo "Installing lxc-install"
	@${STRIP} lxc-install
	@cp -f lxc-install ${BIN_DIR}/lxc-install
	@chmod 755 ${BIN_DIR}/lxc-install
	@mkdir -p ${MAN_DIR}
	@sed "s/VERSION/${VERSION}/g" < ../lxc-install.1 > ${MAN_DIR}/lxc-install.1
	@chmod 644 ${MAN_DIR}/lxc-install.1
	@rm -f lxc-install

uninstall:
	@echo "Uninstalling lxc-install"
	@rm -f ${BIN_DIR}/lxc-install
	@rm -f ${MAN_DIR}/lxc-install.1

clean:
