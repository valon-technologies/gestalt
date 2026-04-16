# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-macos-arm64.tar.gz"
      sha256 "8cf4be0f0556daf1ca125f6b2abc18f793cca9a854cd97db2d585c3078402ba6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-macos-x86_64.tar.gz"
      sha256 "a3f03c5be95f748101cd06266b76a96a2cc374c9cdc2fd414091b97aa9edfe0d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-linux-arm64.tar.gz"
      sha256 "728c3af0a175914cdb0585a54fa74bfc2b139766775cabf5ca6b48d6a297dc55"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.1/gestaltd-linux-x86_64.tar.gz"
      sha256 "0582b38b8cc5d47f81e1984a7c009793a4f22c417f720e3b022ebe4cf8b7c1a2"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
