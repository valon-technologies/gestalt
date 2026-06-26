# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.36"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.36/gestaltd-macos-arm64.tar.gz"
      sha256 "31cca0114bb969f69b539369195f28d43578db78f74c4b18dd2ee4443f849bd7"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.36/gestaltd-macos-x86_64.tar.gz"
      sha256 "3a9def22d68bcfb642302f83dc67e36fc029ae08fd73a623910976040a1b03a6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.36/gestaltd-linux-arm64.tar.gz"
      sha256 "172f18fc4bb407003e7c62ecb8ae95898bd0ec0de09a9a0e011cf9de03c04751"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.36/gestaltd-linux-x86_64.tar.gz"
      sha256 "bc4db6d5675c340ddeb0d4b7635f6c289e8de2cea5475e693188d094e0b2558b"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
