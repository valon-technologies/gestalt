# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.37"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.37/gestaltd-macos-arm64.tar.gz"
      sha256 "c8a030ef9c99b45fbabd4f9c0c2c757e48d680659a59090769ab60152d3c100c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.37/gestaltd-macos-x86_64.tar.gz"
      sha256 "1ef8fc0ea78c01c30b28e801e9e379cd05b40b0b0e885abe6bc41a7ce49788e3"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.37/gestaltd-linux-arm64.tar.gz"
      sha256 "2008636195727e421049e154ca947fa8dbc6f642e7519822e99182d4892605a1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.37/gestaltd-linux-x86_64.tar.gz"
      sha256 "2da4aa86d6e783b246603f90fde129753b3eb45b3319af38074fdcfcc68d5d7a"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
