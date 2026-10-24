_systemd_conf_tui() {
    local cur=${COMP_WORDS[COMP_CWORD]}
    case ${COMP_WORDS[COMP_CWORD-1]} in
        --render) COMPREPLY=( $(compgen -f -- "$cur") ) ;;
        *) COMPREPLY=( $(compgen -W '--help --version --system --render' -- "$cur") ) ;;
    esac
}
complete -F _systemd_conf_tui systemd-conf-tui
