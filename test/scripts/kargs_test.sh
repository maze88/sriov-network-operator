#!/bin/bash
# Copyright 2025 sriov-network-device-plugin authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0



SCRIPTPATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
SUT_SCRIPT="${SCRIPTPATH}/../../bindata/scripts/kargs.sh"


test_RpmOstree_Add_All_Arguments() {
    echo "ID=\"rhel\"" > ${FAKE_HOST}/etc/os-release
    echo "a b c=d eee=fff" > ${FAKE_HOST}/proc/cmdline
    touch ${FAKE_HOST}/run/ostree-booted

    output=`$SUT_SCRIPT add X=Y W=Z`
    assertEquals 0 $?
    assertEquals "2" $output

    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--append X=Y"
    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--append W=Z"
}


test_RpmOstree_Add_Only_Missing_Arguments() {
    echo "ID=\"rhel\"" > ${FAKE_HOST}/etc/os-release
    echo "a b c=d eee=fff K=L" > ${FAKE_HOST}/proc/cmdline
    touch ${FAKE_HOST}/run/ostree-booted

    output=`$SUT_SCRIPT add K=L X=Y`
    assertEquals 0 $?
    assertEquals "1" $output

    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--append X=Y"
    assertNotContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--append K=L"
}

test_RpmOstree_Delete_All_Arguments() {
    echo "ID=\"rhel\"" > ${FAKE_HOST}/etc/os-release
    echo "a b c=d eee=fff X=Y W=Z" > ${FAKE_HOST}/proc/cmdline
    touch ${FAKE_HOST}/run/ostree-booted

    output=`$SUT_SCRIPT remove X=Y W=Z`
    assertEquals 0 $?
    assertEquals "2" $output

    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--delete X=Y"
    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--delete W=Z"
}

test_RpmOstree_Delete_Only_Exist_Arguments() {
    echo "ID=\"rhel\"" > ${FAKE_HOST}/etc/os-release
    echo "a b c=d eee=fff X=Y" > ${FAKE_HOST}/proc/cmdline
    touch ${FAKE_HOST}/run/ostree-booted

    output=`$SUT_SCRIPT remove X=Y W=Z`
    assertEquals 0 $?
    assertEquals "1" $output

    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--delete X=Y"
    assertContains "`cat ${FAKE_HOST}/rpm-ostree_calls`" "--delete W=Z"
}

###### Mock /host directory ######
export FAKE_HOST="$(mktemp -d)"
trap 'rm -rf -- "$FAKE_HOST"' EXIT

setUp() {
    mkdir -p                            ${FAKE_HOST}/{usr/bin,etc,proc,run}
    cp $(which cat)                     ${FAKE_HOST}/usr/bin/
    cp $(which test)                    ${FAKE_HOST}/usr/bin/
    cp $(which sh)                      ${FAKE_HOST}/usr/bin/
    cp $(which grep)                    ${FAKE_HOST}/usr/bin/
    cp "$SCRIPTPATH/rpm-ostree_mock"    ${FAKE_HOST}/usr/bin/rpm-ostree
}

# Mock chroot calls to the temporary test folder
export real_chroot=$(which chroot)
chroot() {
    $real_chroot $FAKE_HOST "${@:2}"
}
export -f chroot


source ${SCRIPTPATH}/shunit2
