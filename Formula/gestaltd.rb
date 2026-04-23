# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.3/gestaltd-macos-arm64.tar.gz"
      sha256 "2cdb303e0e6ad3375f83965cb75aab1a86cb3893311bcea08ee66ab41bfba547"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.3/gestaltd-macos-x86_64.tar.gz"
      sha256 "7966e229697208e6367685f05a9f328d12f57ddbe0ac1c63e45a9f7e0727223a"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.3/gestaltd-linux-arm64.tar.gz"
      sha256 "56cd541594feea744763739a437eb9fbbeb1844db90e090b25360ad704dce719"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.3/gestaltd-linux-x86_64.tar.gz"
      sha256 "f6eeea2b9c888ec3be7426a096aca7e75a8cba6ca93bc0a258639ad96dc72db9"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
