Name:           laststate-relay
Version:        0.3.0
Release:        1%{?dist}
Summary:        Offline-first Latch to Trace diagnostics gateway
License:        Apache-2.0
URL:            https://github.com/laststate/relay
Source0:        %{name}-%{version}.tar.gz
BuildRequires:  golang >= 1.22

%description
Last State Relay accepts LEP envelopes, persists them durably, and forwards
them to Trace with retries, batching, and offline operation.

%prep
%autosetup

%build
go build -trimpath -ldflags "-s -w -X main.cliVersion=v%{version}" -o laststate-relay ./cmd/laststate-relay

%install
install -D -m 0755 laststate-relay %{buildroot}/usr/local/bin/laststate-relay
install -D -m 0644 packaging/systemd/laststate-relay.service %{buildroot}/lib/systemd/system/laststate-relay.service
install -D -m 0644 relay.yaml.example %{buildroot}/etc/laststate/relay.yaml
install -D -m 0644 packaging/udev/99-laststate.rules %{buildroot}/etc/udev/rules.d/99-laststate.rules

%files
/usr/local/bin/laststate-relay
/lib/systemd/system/laststate-relay.service
%config(noreplace) /etc/laststate/relay.yaml
/etc/udev/rules.d/99-laststate.rules

%changelog
* Wed Jul 15 2026 Last State contributors - 0.3.0-1
- Onda 1 packaging template
