# oc adm release info --image-for rhel-coreos on a 4.17 cluster
# default image, but please use "$make kubesan-node-layering-image" to build correct image with current cluster's rhel version
ARG BASE_IMG=quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:eaa7835f2ec7d2513a76e30a41c21ce62ec11313fab2f8f3f46dd4999957a883

FROM $BASE_IMG

ADD ./appstream.repo /etc/yum.repos.d/
ADD ./baseos.repo /etc/yum.repos.d/

ADD ./lvmlockd_generate.sh /usr/bin/
RUN chmod +x /usr/bin/lvmlockd_generate.sh
ADD ./generate-lvmlockd-config.service /etc/systemd/system/

RUN rpm-ostree cliwrap install-to-root / && \
    rpm-ostree install lvm2-lockd sanlock && \
    rpm-ostree cleanup -m && \
    mkdir -p /etc/modules-load.d && echo -e "nbd\ndm-thin-pool" | sudo tee /etc/modules-load.d/kubesan.conf && \
    mkdir -p /etc/sanlock && echo -e "use_watchdog = 0" | sudo tee /etc/sanlock/sanlock.conf && \
    sed -i 's/^[[:space:]]*# use_lvmlockd = 0[[:space:]]*$/use_lvmlockd = 1/' /etc/lvm/lvm.conf && \
    systemctl enable generate-lvmlockd-config.service sanlock lvmlockd && \
    ostree container commit

RUN mkdir -p /etc/systemd/system/sanlock.service.d
ADD ./sanlock-root-workaround.conf /etc/systemd/system/sanlock.service.d/override.conf
