%global service_name ablestack-api
%global config_root %{_sysconfdir}/ablestack
%global state_root %{config_root}/vmconfig
%global api_port 8090
%global debug_package %{nil}
%{!?_unitdir:%global _unitdir %{_prefix}/lib/systemd/system}

%bcond_without tests

Name:           ablestack-api
Version:        %{?rpm_version}%{!?rpm_version:0.1.1}
Release:        %{?rpm_release}%{!?rpm_release:1}%{?dist}
Summary:        ABLESTACK API server
License:        Apache-2.0
URL:            https://www.ablecloud.io
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang
BuildRequires:  libvirt-devel
BuildRequires:  pkgconfig
BuildRequires:  systemd-rpm-macros
Requires:       systemd
Requires:       python3
Recommends:     firewalld
Requires(post): systemd
Requires(post): python3
Requires(preun): systemd
Requires(postun): systemd

%description
ABLESTACK API server and managed configuration files.

%prep
%autosetup -n %{name}-%{version}

%build
export GO111MODULE=on
export CGO_ENABLED=1
if [ -d vendor ]; then
    export GOFLAGS="${GOFLAGS:-} -mod=vendor"
fi
go build -buildvcs=false -trimpath -ldflags "-s -w" -o %{name} ./cmd/apiserver
go build -buildvcs=false -trimpath -ldflags "-s -w" -o ablestack-auth-token ./cmd/authtoken

%check
%if %{with tests}
go test ./internal/model/cube ./internal/service/clusterconfig ./internal/service/authservice ./cmd/authtoken ./docs
%endif

%install
install -Dpm 0755 %{name} %{buildroot}%{_bindir}/%{name}
install -Dpm 0755 ablestack-auth-token %{buildroot}%{_bindir}/ablestack-auth-token
install -Dpm 0644 packaging/systemd/%{service_name}.service %{buildroot}%{_unitdir}/%{service_name}.service
install -Dpm 0755 packaging/scripts/merge-json-defaults.py %{buildroot}%{_libexecdir}/%{name}/merge-json-defaults.py

install -d %{buildroot}%{config_root}
install -Dpm 0644 configs/config.json %{buildroot}%{config_root}/config.json
install -Dpm 0600 configs/auth.json %{buildroot}%{config_root}/auth.json
install -Dpm 0644 packaging/config/ablestack-api.env %{buildroot}%{config_root}/ablestack-api.env

install -d %{buildroot}%{config_root}/properties
install -pm 0644 properties/* %{buildroot}%{config_root}/properties/

install -d %{buildroot}%{config_root}/xml-template
install -pm 0644 xml-template/* %{buildroot}%{config_root}/xml-template/

install -d %{buildroot}%{config_root}/shell
install -pm 0755 shell/* %{buildroot}%{config_root}/shell/

install -d %{buildroot}%{state_root}/ccvm
install -d %{buildroot}%{state_root}/scvm

%post
merge_json_if_rpmnew() {
    target="$1"
    defaults="${target}.rpmnew"
    if [ -f "$target" ] && [ -f "$defaults" ]; then
        %{_libexecdir}/%{name}/merge-json-defaults.py "$target" "$defaults" >/dev/null 2>&1 || :
        rm -f "$defaults" || :
    fi
}
merge_json_if_rpmnew "%{config_root}/config.json"
merge_json_if_rpmnew "%{config_root}/auth.json"
merge_json_if_rpmnew "%{config_root}/properties/cluster.json"

configure_firewall() {
    if ! command -v firewall-cmd >/dev/null 2>&1; then
        return 0
    fi

    if command -v systemctl >/dev/null 2>&1; then
        systemctl enable --now firewalld.service >/dev/null 2>&1 || :
    fi

    firewall-cmd --permanent --add-port=%{api_port}/tcp >/dev/null 2>&1 || :
    firewall-cmd --add-port=%{api_port}/tcp >/dev/null 2>&1 || :
    firewall-cmd --reload >/dev/null 2>&1 || :
}
configure_firewall

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || :
    systemctl enable --now %{service_name}.service >/dev/null 2>&1 || :
    if [ "$1" -gt 1 ]; then
        systemctl try-restart %{service_name}.service >/dev/null 2>&1 || :
    fi
fi

%preun
if [ "$1" -eq 0 ] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now %{service_name}.service >/dev/null 2>&1 || :
fi

%postun
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || :
fi

%files
%license LICENSE
%doc README.md CHANGELOG.md VERSION
%{_bindir}/%{name}
%{_bindir}/ablestack-auth-token
%{_unitdir}/%{service_name}.service
%{_libexecdir}/%{name}/merge-json-defaults.py
%dir %{config_root}
%config(noreplace) %{config_root}/config.json
%config(noreplace) %attr(0600,root,root) %{config_root}/auth.json
%config(noreplace) %{config_root}/ablestack-api.env
%dir %{config_root}/properties
%config(noreplace) %{config_root}/properties/*
%dir %{config_root}/xml-template
%config(noreplace) %{config_root}/xml-template/*
%dir %{config_root}/shell
%config(noreplace) %attr(0755,root,root) %{config_root}/shell/*
%dir %{state_root}
%dir %{state_root}/ccvm
%dir %{state_root}/scvm

%changelog
* Tue May 26 2026 ABLECLOUD <support@ablecloud.io> - 0.1.1-1
- Add Cockpit session token helper CLI.
- Add deployment status API for UI stage handling.
- Add dynamic PCS cluster target handling up to 16 hosts.
- Separate PCS validation rules by ABLESTACK deployment type.
- Improve PCS-based CloudCenter status, snapshot, and Glue config flows.
- Update API documentation and Swagger output for deployment status.

* Tue May 26 2026 ABLECLOUD <support@ablecloud.io> - 0.1.0-1
- Add RPM packaging for ABLESTACK API.
- Use VERSION as the RPM build version source.
- Include CHANGELOG and VERSION in package documentation.
