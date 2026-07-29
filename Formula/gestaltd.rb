# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.52"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.52/gestaltd-macos-arm64.tar.gz"
      sha256 "5fa8d8e3eb0492ceb342a0ff9453c389ec9a237cf75b55f61f9b0793643670e2"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.52/gestaltd-macos-x86_64.tar.gz"
      sha256 "dd6ec0041ed87b2e905055f1e2a6ae9332325b7ad77f0e833d14b1541135610b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.52/gestaltd-linux-arm64.tar.gz"
      sha256 "6701ba0227781df0be8e477a13ff164da52980620fc65f0366a2c3d359e7282d"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.52/gestaltd-linux-x86_64.tar.gz"
      sha256 "d94804b1f4a9350c63750eb4a5d8fa36aca07fed10388fc03289229e594b5819"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
