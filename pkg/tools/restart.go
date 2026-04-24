package tools

type RestartHandler func() (string, error)

var restartHandler RestartHandler

func SetRestartHandler(handler RestartHandler) {
	restartHandler = handler
}

func handleRestartCommand(command string) (string, bool, error) {
	if command != "mini-code restart" {
		return "", false, nil
	}
	if restartHandler == nil {
		return "", true, nil
	}

	result, err := restartHandler()
	return result, true, err
}
